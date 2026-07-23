import { reactive } from 'vue';

export const dialogState = reactive({
  open: false,
  mode: 'confirm',
  title: '',
  message: '',
  confirmLabel: '确认',
  cancelLabel: '取消',
  tone: 'default',
  value: '',
  placeholder: '',
});

let resolver = null;

function showDialog(options) {
  if (resolver) resolver(options.mode === 'prompt' ? null : false);
  Object.assign(dialogState, {
    open: true,
    mode: options.mode || 'confirm',
    title: options.title || (options.mode === 'prompt' ? '请输入内容' : '请确认'),
    message: options.message || '',
    confirmLabel: options.confirmLabel || '确认',
    cancelLabel: options.cancelLabel || '取消',
    tone: options.tone || 'default',
    value: options.defaultValue || '',
    placeholder: options.placeholder || '',
  });
  return new Promise((resolve) => { resolver = resolve; });
}

export function confirmDialog(messageOrOptions) {
  const options = typeof messageOrOptions === 'string' ? { message: messageOrOptions } : messageOrOptions;
  return showDialog({ ...options, mode: 'confirm' });
}

export function promptDialog(options) {
  return showDialog({ ...(typeof options === 'string' ? { message: options } : options), mode: 'prompt' });
}

export function resolveDialog(confirmed) {
  const done = resolver;
  resolver = null;
  const result = dialogState.mode === 'prompt'
    ? (confirmed ? dialogState.value.trim() : null)
    : Boolean(confirmed);
  dialogState.open = false;
  if (done) done(result);
}
