package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 自动更新源：GitHub Release。版本号由构建参数注入（-ldflags "-X main.appVersion=x.y.z"）。
const (
	appRepo       = "sala-coding/LLM_Router"
	appReleaseURL = "https://github.com/" + appRepo + "/releases/latest"
	appAPIURL     = "https://api.github.com/repos/" + appRepo + "/releases/latest"
)

var (
	updateAPIClient = &http.Client{Timeout: 20 * time.Second}
	updateHTTP      = &http.Client{Timeout: 15 * time.Minute}
)

type ghRelease struct {
	TagName string    `json:"tag_name"`
	HTMLURL string    `json:"html_url"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// CheckAppUpdate 查询 GitHub 最新 Release，并与当前版本比较。
func (a *App) CheckAppUpdate() map[string]any {
	rel, err := fetchLatestRelease()
	if err != nil {
		return map[string]any{"ok": false, "currentVersion": appVersion, "error": err.Error()}
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	info := map[string]any{
		"ok":              true,
		"currentVersion":  appVersion,
		"latestVersion":   latest,
		"releaseURL":      firstNonEmpty(rel.HTMLURL, appReleaseURL),
		"notes":           trimNotes(rel.Body),
		"updateAvailable": versionLess(appVersion, latest),
	}
	asset := pickUpdateAsset(rel.Assets)
	if asset.URL != "" {
		info["downloadURL"] = asset.URL
		info["assetSize"] = asset.Size
	}
	return info
}

func fetchLatestRelease() (*ghRelease, error) {
	req, err := http.NewRequest(http.MethodGet, appAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "LLM-Router/"+appVersion)
	resp, err := updateAPIClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查更新失败（HTTP %d）", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// pickUpdateAsset 优先选 Windows exe，其次 zip（下载后自动解包）。
func pickUpdateAsset(assets []ghAsset) ghAsset {
	var exe, zipAsset ghAsset
	best := -1
	for _, as := range assets {
		name := strings.ToLower(as.Name)
		if as.URL == "" {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".exe"):
			score := 0
			if strings.Contains(name, "llm") || strings.Contains(name, "router") {
				score += 2
			}
			if strings.Contains(name, "windows") || strings.Contains(name, "win") {
				score++
			}
			if score > best {
				best, exe = score, as
			}
		case strings.HasSuffix(name, ".zip") && zipAsset.URL == "":
			zipAsset = as
		}
	}
	if exe.URL != "" {
		return exe
	}
	return zipAsset
}

func trimNotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[:4000] + "\n…"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func versionLess(current, latest string) bool {
	if strings.TrimSpace(latest) == "" {
		return false
	}
	a, b := versionNums(current), versionNums(latest)
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func versionNums(v string) []int {
	v = strings.TrimLeft(strings.TrimSpace(v), "vV")
	var out []int
	cur := ""
	for _, r := range v {
		if r >= '0' && r <= '9' {
			cur += string(r)
			continue
		}
		if cur != "" {
			n, _ := strconv.Atoi(cur)
			out = append(out, n)
			cur = ""
		}
	}
	if cur != "" {
		n, _ := strconv.Atoi(cur)
		out = append(out, n)
	}
	return out
}

// DownloadAndInstallUpdate 下载更新包，校验后启动替换脚本并退出当前进程。
// 下载进度通过 Wails 事件 app:update:progress 推送（percent/received/total）。
func (a *App) DownloadAndInstallUpdate(rawURL string) map[string]any {
	if runtime.GOOS != "windows" {
		return map[string]any{"ok": false, "error": "自动更新仅支持 Windows"}
	}
	if !allowedUpdateURL(rawURL) {
		return map[string]any{"ok": false, "error": "非法的下载地址"}
	}
	exePath, err := os.Executable()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	exePath, _ = filepath.Abs(exePath)
	if err := checkDirWritable(filepath.Dir(exePath)); err != nil {
		return map[string]any{"ok": false, "error": "程序目录不可写，请打开页面手动下载安装"}
	}

	base := fmt.Sprintf("llm-router-update-%d", time.Now().UnixNano())
	pkgPath := filepath.Join(os.TempDir(), base+updateExt(rawURL))
	if err := downloadUpdate(a.ctx, rawURL, pkgPath); err != nil {
		_ = os.Remove(pkgPath)
		return map[string]any{"ok": false, "error": err.Error()}
	}

	newExe := pkgPath
	cleanupDir := ""
	if strings.HasSuffix(strings.ToLower(pkgPath), ".zip") {
		cleanupDir = filepath.Join(os.TempDir(), base)
		newExe, err = unzipUpdate(pkgPath, cleanupDir)
		if err != nil {
			_ = os.Remove(pkgPath)
			_ = os.RemoveAll(cleanupDir)
			return map[string]any{"ok": false, "error": "更新包解压失败：" + err.Error()}
		}
	}
	if err := verifyUpdateExe(newExe); err != nil {
		_ = os.Remove(pkgPath)
		_ = os.RemoveAll(cleanupDir)
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if err := launchUpdateHelper(exePath, newExe, pkgPath, cleanupDir); err != nil {
		return map[string]any{"ok": false, "error": "启动安装程序失败：" + err.Error()}
	}
	if a.ctx != nil {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			a.forceQuit.Store(true) // 绕过托盘常驻的「关闭即隐藏」
			wailsruntime.Quit(a.ctx)
		}()
	}
	return map[string]any{"ok": true, "restarting": true}
}

func updateExt(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ".exe"
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	if ext == ".zip" {
		return ".zip"
	}
	return ".exe"
}

func allowedUpdateURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" ||
		host == "release-assets.githubusercontent.com" ||
		host == "objects.githubusercontent.com" ||
		host == "github-releases.githubusercontent.com"
}

func downloadUpdate(ctx context.Context, rawURL, dst string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LLM-Router/"+appVersion)
	resp, err := updateHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败（HTTP %d）", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var received int64
	buf := make([]byte, 128*1024)
	lastPct, lastEmit := -2, time.Time{}
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			received += int64(n)
			pct := -1
			if total > 0 {
				pct = int(received * 100 / total)
			}
			if pct != lastPct || time.Since(lastEmit) > 200*time.Millisecond {
				emitUpdateProgress(ctx, pct, received, total)
				lastPct, lastEmit = pct, time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	emitUpdateProgress(ctx, 100, received, total)
	return f.Close()
}

func emitUpdateProgress(ctx context.Context, pct int, received, total int64) {
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, "app:update:progress", map[string]any{
		"percent": pct, "received": received, "total": total,
	})
}

// checkDirWritable 预先确认程序目录可写，否则更新必然失败，提前给出提示。
func checkDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".llm-router-update-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

func verifyUpdateExe(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil || string(head) != "MZ" {
		return fmt.Errorf("下载的文件不是有效的 Windows 程序")
	}
	if st, err := f.Stat(); err == nil && st.Size() < 64*1024 {
		return fmt.Errorf("下载的文件不完整")
	}
	return nil
}

func unzipUpdate(zipPath, dstDir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	var best string
	for _, zf := range zr.File {
		name := strings.ToLower(zf.Name)
		if !strings.HasSuffix(name, ".exe") || zf.FileInfo().IsDir() {
			continue
		}
		out := filepath.Join(dstDir, filepath.Base(zf.Name))
		if err := extractZipEntry(zf, out); err != nil {
			return "", err
		}
		score := 0
		if strings.Contains(name, "llm") || strings.Contains(name, "router") {
			score = 1
		}
		if best == "" || score > 0 {
			best = out
		}
		if score > 0 {
			break
		}
	}
	if best == "" {
		return "", fmt.Errorf("压缩包内未找到 exe")
	}
	return best, nil
}

func extractZipEntry(zf *zip.File, dst string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// launchUpdateHelper 生成替换脚本：等待当前进程退出 → 备份并替换 exe → 启动新版本。
func launchUpdateHelper(exePath, newExe, pkgPath, cleanupDir string) error {
	bat := filepath.Join(os.TempDir(), fmt.Sprintf("llm-router-update-%d.bat", time.Now().UnixNano()))
	oldExe := exePath + ".old"
	script := fmt.Sprintf(`@echo off
setlocal
set "PID=%d"
:wait
tasklist /FI "PID eq %%PID%%" /NH 2>nul | find "%%PID%%" >nul
if not errorlevel 1 (
  ping 127.0.0.1 -n 2 >nul
  goto wait
)
move /Y "%s" "%s" >nul 2>&1
move /Y "%s" "%s" >nul 2>&1
if not exist "%s" move /Y "%s" "%s" >nul 2>&1
start "" "%s"
del "%s" >nul 2>&1
del "%s" >nul 2>&1
if not "%s"=="" rmdir /S /Q "%s" >nul 2>&1
(goto) 2>nul & del "%%~f0"
`,
		os.Getpid(),
		exePath, oldExe,
		newExe, exePath,
		exePath, oldExe, exePath,
		exePath,
		oldExe,
		pkgPath,
		cleanupDir, cleanupDir,
	)
	if err := os.WriteFile(bat, []byte(script), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", bat)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
