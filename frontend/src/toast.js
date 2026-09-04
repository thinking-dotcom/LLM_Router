import { reactive } from 'vue'

export const toastState = reactive({ msg: '', visible: false })
let timer = null

export function toast(msg, ms = 2000) {
  toastState.msg = msg
  toastState.visible = true
  clearTimeout(timer)
  timer = setTimeout(() => { toastState.visible = false }, ms)
}
