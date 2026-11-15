<script setup>
// Deep Code CLI 配置面板（从 Clients.vue 抽出）：接入地址 / 模型 / 内置 ID 映射 / 思考模式。
// 状态全部来自 sync/clients/deepcode.js 的模块级 ref，无需 props。
import { t } from '../../../i18n'
import { delay } from '../../../utils'
import { routerHintForTarget } from '../store'
import { modelGroupsByLetter } from '../model-select'
import { optTxt } from '../ui'
import {
  deepcodeForm, deepcodeModelOpen, pickDeepcodeModel, DEEPCODE_EFFORTS,
  DEEPCODE_BUILTIN_MODELS, deepcodeAliasMap, deepcodeSetAlias,
  deepcodeToOpenFor, deepcodeToFilter, deepcodeToGroups, pickDeepcodeTo, deepcodeToKeyNav,
} from '../clients/deepcode'
</script>

<template>
  <div class="panel-head">{{ t('sync.panelModel') }}</div>
  <section class="card cfg-card dc-lab-left">
    <div class="field fw-l">
      <label :title="t('sync.deepcode.baseUrlTip')">{{ t('sync.baseUrl') }}</label>
      <input :value="deepcodeForm.baseUrl || routerHintForTarget()" @input="e=>deepcodeForm.baseUrl=e.target.value" :placeholder="routerHintForTarget()" />
    </div>
    <div class="field-row">
      <div class="field fw-m">
        <label :title="t('sync.deepcode.apiKeyTip')">{{ t('sync.apiKey') }}</label>
        <input :value="deepcodeForm.apiKey" @input="e=>deepcodeForm.apiKey=e.target.value" :placeholder="t('sync.routeToken')" />
      </div>
      <div class="field fw-m">
        <label :title="t('sync.deepcode.modelTip')">{{ t('sync.model') }}</label>
        <div class="pi-sel">
          <input v-model="deepcodeForm.model" :placeholder="t('sync.pickModel')" @focus="deepcodeModelOpen=true" @click.stop="deepcodeModelOpen=true" @input="deepcodeModelOpen=true" @blur="delay(()=>{ if(deepcodeModelOpen) deepcodeModelOpen=false },120)" />
          <div v-show="deepcodeModelOpen" class="pi-sel-drop">
            <div v-for="g in modelGroupsByLetter(deepcodeForm.model)" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
              <div class="pi-sel-letter">{{ g.letter }}</div>
              <button v-for="m in g.items" :key="m" type="button" @mousedown.prevent="pickDeepcodeModel(m)">{{ m }}</button>
            </div>
            <div v-if="!modelGroupsByLetter(deepcodeForm.model).length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 内置 ID 映射（并入「模型配置」卡）：CLI 的 /model 菜单只列这 4 个内置 ID，
         且菜单里选中会把顶层 model 改成该 ID；这里把它们映射到上游模型
         （写服务商 aliases），菜单里任选一项都能路由到真实模型 -->
    <div class="field fw-l dc-map-field">
      <label :title="t('sync.deepcode.mapTip')">{{ t('sync.deepcode.mapPanel') }}</label>
      <div class="agy-map-main deepcode-map">
        <div class="agy-map-list">
          <div class="agy-map-rows">
            <div v-for="m in DEEPCODE_BUILTIN_MODELS" :key="m" class="agy-map-row">
              <span class="agy-map-builtin">{{ m }}</span>
              <div class="pi-sel agy-map-val">
                <input :value="deepcodeAliasMap[m] || ''" :placeholder="t('sync.deepcode.mapToPh')"
                  @input="deepcodeSetAlias(m, $event.target.value); deepcodeToOpenFor = m; deepcodeToFilter = $event.target.value"
                  @focus="deepcodeToOpenFor = m; deepcodeToFilter = ''"
                  @blur="delay(() => { if (deepcodeToOpenFor === m) deepcodeToOpenFor = '' }, 140)"
                  @keydown="deepcodeToKeyNav" />
                <div v-show="deepcodeToOpenFor === m" class="pi-sel-drop">
                  <div v-for="g in deepcodeToGroups()" :key="g.letter" class="pi-sel-group" :data-letter="g.letter">
                    <div class="pi-sel-letter">{{ g.letter }}</div>
                    <button v-for="opt in g.items" :key="opt" type="button" @mousedown.prevent="pickDeepcodeTo(m, opt)">{{ opt }}</button>
                  </div>
                  <div v-if="!deepcodeToGroups().length" class="pi-sel-empty">{{ t('sync.noMatch') }}</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>

  <div class="panel-head">{{ t('sync.deepcode.thinkingPanel') }}</div>
  <section class="card cfg-card dc-lab-left">
    <div class="field-row">
      <div class="field fw-s">
        <label>{{ t('sync.deepcode.thinkingEnabled') }}</label>
        <select :value="deepcodeForm.thinkingEnabled ? 'on' : 'off'" @change="e=>deepcodeForm.thinkingEnabled = e.target.value === 'on'">
          <option value="on">{{ t('sync.deepcode.on') }}</option>
          <option value="off">{{ t('sync.deepcode.off') }}</option>
        </select>
      </div>
      <div class="field fw-s">
        <label :title="t('sync.deepcode.effortTip')">{{ t('sync.deepcode.effort') }}</label>
        <select :value="deepcodeForm.reasoningEffort" @change="e=>deepcodeForm.reasoningEffort=e.target.value">
          <option v-for="lv in DEEPCODE_EFFORTS" :key="lv" :value="lv">{{ optTxt(lv) }}</option>
        </select>
      </div>
    </div>

    <div class="dc-note">{{ t('sync.deepcode.tip') }}</div>
  </section>
</template>

<style scoped>
@import './panel.css';
</style>
