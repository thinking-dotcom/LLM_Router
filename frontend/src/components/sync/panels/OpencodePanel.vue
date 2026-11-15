<script setup>
// OpenCode 配置面板（从 Clients.vue 抽出）：模型/provider 与权限胶囊。
// 状态全部来自 sync/clients/opencode.js 的模块级 ref，无需 props。
import { t } from '../../../i18n'
import { copyText, delay } from '../../../utils'
import { derivedBaseUrl } from '../store'
import { modelGroupsByLetter } from '../model-select'
import { ocModelOpen, pickOcModel } from '../controller'
import {
  ocAccessMode, ocApiKey, ocNpm, ocName, ocModels, OC_PERM_KEYS, ocPerms, ocCyclePerm,
  ocForm, ocMainOpen, ocSmallOpen, pickOcMainModel, pickOcSmallModel, OC_AGENTS,
} from '../clients/opencode'
import { permCapCls, permCapWord } from '../ui'
</script>

<template>
  <div class="panel-head">{{ t('sync.panelModel') }}</div>
  <section class="card cfg-card">
    <div class="field-row">
      <div class="field fw-l"><label title="provider.llm">{{ t('sync.baseUrl') }}</label>
        <div class="ro-url"><input :value="derivedBaseUrl(ocAccessMode)" readonly /><button type="button" class="btn btn-xs btn-ghost" @click="copyText(derivedBaseUrl(ocAccessMode), $event.currentTarget)">{{ t('sync.copy') }}</button></div>
      </div>
      <div class="field fw-m"><label>{{ t('sync.apiKey') }}</label><input v-model="ocApiKey" :placeholder="t('sync.routeToken')" /></div>
    </div>
    <div class="field-row">
      <div class="field fw-m"><label>{{ t('sync.npm') }}</label><input v-model="ocNpm" placeholder="@ai-sdk/openai-compatible" /></div>
      <div class="field fw-m"><label>{{ t('sync.name') }}</label><input v-model="ocName" placeholder="LLM" /></div>
    </div>
    <div class="field"><label>{{ t('sync.oc.modelSet') }}</label>
      <div v-for="(row, i) in ocModels" :key="row" class="model-row">
        <div class="field fw-l"><label>{{ t('sync.id') }}</label>
          <div class="pi-sel">
            <input v-model="row.id" :placeholder="t('sync.pickModel')" @focus="ocModelOpen=i" @click.stop="ocModelOpen=i" @input="ocModelOpen=i" @blur="delay(()=>{ if(ocModelOpen===i) ocModelOpen=-1 },120)" />
            <div v-show="ocModelOpen===i" class="pi-sel-drop">
              <div v-for="g in modelGroupsByLetter(row.id)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                <div class="pi-sel-letter">{{ g.letter }}</div>
                <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickOcModel(i, m)">{{ m }}</button>
              </div>
              <div v-if="!modelGroupsByLetter(row.id).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
            </div>
          </div>
        </div>
        <div class="field fw-m"><label>{{ t('sync.oc.displayName') }}</label><input v-model="row.name" /></div>
        <div class="field fw-s"><label>{{ t('sync.thinking') }}</label>
          <select v-model="row.reasoning"><option value="">{{ t('sync.default') }}</option><option value="minimal">{{ t('sync.opt.minimal') }}</option><option value="low">{{ t('sync.opt.low') }}</option><option value="medium">{{ t('sync.opt.medium') }}</option><option value="high">{{ t('sync.opt.high') }}</option></select>
        </div>
        <div class="field fw-s"><label :title="t('sync.samplingTemp')">{{ t('sync.temperature') }}</label><input type="number" step="0.1" min="0" max="2" v-model="row.temperature" :placeholder="t('sync.phExample', { v: '0.7' })" /></div>
        <button type="button" class="btn btn-xs btn-ghost row-del" @click="ocModels.splice(i,1)">×</button>
      </div>
      <button type="button" class="btn btn-xs btn-ghost" @click="ocModels.push({ id: '', name: '', reasoning: '', temperature: '' })">{{ t('sync.addModel') }}</button>
    </div>
  </section>
  <div class="panel-head" :title="t('sync.oc.advTip')">{{ t('sync.oc.advPanel') }}</div>
  <section class="card cfg-card">
    <div class="field-row">
      <div class="field fw-l">
        <label :title="t('sync.oc.mainModelTip')">{{ t('sync.oc.mainModel') }}</label>
        <div class="pi-sel">
          <input v-model="ocForm.model" :placeholder="t('sync.oc.smallModelPh')" @focus="ocMainOpen=true" @click.stop="ocMainOpen=true" @input="ocMainOpen=true" @blur="delay(()=>{ if(ocMainOpen) ocMainOpen=false },120)" />
          <div v-show="ocMainOpen" class="pi-sel-drop">
            <div v-for="g in modelGroupsByLetter(ocForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
              <div class="pi-sel-letter">{{ g.letter }}</div>
              <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickOcMainModel(m)">{{ m }}</button>
            </div>
            <div v-if="!modelGroupsByLetter(ocForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
          </div>
        </div>
      </div>
      <div class="field fw-l">
        <label :title="t('sync.oc.smallModelTip')">{{ t('sync.oc.smallModel') }}</label>
        <div class="pi-sel">
          <input v-model="ocForm.smallModel" :placeholder="t('sync.oc.smallModelPh')" @focus="ocSmallOpen=true" @click.stop="ocSmallOpen=true" @input="ocSmallOpen=true" @blur="delay(()=>{ if(ocSmallOpen) ocSmallOpen=false },120)" />
          <div v-show="ocSmallOpen" class="pi-sel-drop">
            <div v-for="g in modelGroupsByLetter(ocForm.smallModel)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
              <div class="pi-sel-letter">{{ g.letter }}</div>
              <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickOcSmallModel(m)">{{ m }}</button>
            </div>
            <div v-if="!modelGroupsByLetter(ocForm.smallModel).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
          </div>
        </div>
      </div>
      <div class="field fw-m">
        <label :title="t('sync.oc.agentTip')">{{ t('sync.oc.agent') }}</label>
        <select v-model="ocForm.defaultAgent">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option v-for="a in OC_AGENTS" :key="a" :value="a">{{ a }}</option>
        </select>
      </div>
    </div>
    <div class="field-row">
      <div class="field fw-m">
        <label :title="t('sync.oc.autoupdateTip')">{{ t('sync.oc.autoupdate') }}</label>
        <select v-model="ocForm.autoupdate">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="true">{{ t('sync.opt.true') }}</option>
          <option value="false">{{ t('sync.opt.false') }}</option>
          <option value="notify">{{ t('sync.oc.auNotify') }}</option>
        </select>
      </div>
      <div class="field fw-m">
        <label :title="t('sync.oc.shareTip')">{{ t('sync.oc.share') }}</label>
        <select v-model="ocForm.share">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="manual">{{ t('sync.oc.shareManual') }}</option>
          <option value="auto">{{ t('sync.oc.shareAuto') }}</option>
          <option value="disabled">{{ t('sync.oc.shareOff') }}</option>
        </select>
      </div>
      <div class="field fw-s">
        <label :title="t('sync.oc.snapshotTip')">{{ t('sync.oc.snapshot') }}</label>
        <select v-model="ocForm.snapshot">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="true">{{ t('sync.opt.true') }}</option>
          <option value="false">{{ t('sync.opt.false') }}</option>
        </select>
      </div>
    </div>
    <div class="field-row">
      <div class="field fw-s">
        <label :title="t('sync.oc.cmpAutoTip')">{{ t('sync.oc.cmpAuto') }}</label>
        <select v-model="ocForm.cmpAuto">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="true">{{ t('sync.opt.true') }}</option>
          <option value="false">{{ t('sync.opt.false') }}</option>
        </select>
      </div>
      <div class="field fw-s">
        <label :title="t('sync.oc.cmpPruneTip')">{{ t('sync.oc.cmpPrune') }}</label>
        <select v-model="ocForm.cmpPrune">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="true">{{ t('sync.opt.true') }}</option>
          <option value="false">{{ t('sync.opt.false') }}</option>
        </select>
      </div>
      <div class="field fw-s"><label :title="t('sync.oc.cmpReservedTip')">{{ t('sync.oc.cmpReserved') }}</label><input v-model="ocForm.cmpReserved" inputmode="numeric" placeholder="10000" /></div>
      <div class="field fw-s">
        <label :title="t('sync.oc.toolWriteTip')">{{ t('sync.oc.toolWrite') }}</label>
        <select v-model="ocForm.toolsWrite">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="true">{{ t('sync.opt.true') }}</option>
          <option value="false">{{ t('sync.opt.false') }}</option>
        </select>
      </div>
      <div class="field fw-s">
        <label :title="t('sync.oc.toolBashTip')">{{ t('sync.oc.toolBash') }}</label>
        <select v-model="ocForm.toolsBash">
          <option value="">{{ t('sync.notWrite') }}</option>
          <option value="true">{{ t('sync.opt.true') }}</option>
          <option value="false">{{ t('sync.opt.false') }}</option>
        </select>
      </div>
    </div>
  </section>

  <div class="panel-head">{{ t('sync.panelPerm') }}</div>
  <section class="card cfg-card">
    <div class="perm-caps" role="group" :aria-label="t('sync.panelPerm')">
      <button v-for="p in OC_PERM_KEYS" :key="p.k" type="button" class="perm-cap" role="radio"
        :class="permCapCls(ocPerms[p.k].mode)" :aria-checked="ocPerms[p.k].mode!==''"
        :title="ocPerms[p.k].obj ? t('sync.permObjLock') : t(p.hintKey) + ' · ' + t('sync.permCapClick')" @click="ocCyclePerm(p.k)">
        <span class="perm-cap-dot"></span>
        <span class="perm-cap-text">{{ p.k }}</span>
        <span class="perm-cap-state">{{ permCapWord(ocPerms[p.k].mode, ocPerms[p.k].obj) }}</span>
      </button>
    </div>
  </section>
</template>

<style scoped>
@import './panel.css';
</style>
