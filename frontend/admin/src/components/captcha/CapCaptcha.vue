<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import 'cap-widget'
import type { CapWidget } from 'cap-widget'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  modelValue?: string
  endpoint: string
  disabled?: boolean
}>(), {
  modelValue: '',
  disabled: false,
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
}>()

const widgetRef = ref<CapWidget | null>(null)

const handleSolve = (event: Event) => {
  const detail = (event as CustomEvent<{ token?: string }>).detail
  emit('update:modelValue', String(detail?.token || ''))
}

const clearToken = () => {
  emit('update:modelValue', '')
}

const reset = () => {
  widgetRef.value?.reset()
  clearToken()
}

watch(() => props.endpoint, () => {
  clearToken()
})

defineExpose({ reset })
</script>

<template>
  <div class="min-h-[42px]" :class="{ 'pointer-events-none opacity-60': disabled }">
    <cap-widget
      v-if="endpoint"
      ref="widgetRef"
      :key="endpoint"
      required
      data-cap-disable-haptics
      :data-cap-api-endpoint="endpoint"
      :data-cap-i18n-initial-state="t('admin.login.cap.initial')"
      :data-cap-i18n-verifying-label="t('admin.login.cap.verifying')"
      :data-cap-i18n-solved-label="t('admin.login.cap.solved')"
      :data-cap-i18n-error-label="t('admin.login.cap.error')"
      :data-cap-i18n-verify-aria-label="t('admin.login.cap.verifyAria')"
      :data-cap-i18n-verifying-aria-label="t('admin.login.cap.verifyingAria')"
      :data-cap-i18n-verified-aria-label="t('admin.login.cap.verifiedAria')"
      :data-cap-i18n-wasm-disabled="t('admin.login.cap.wasmDisabled')"
      :data-cap-i18n-required-label="t('admin.login.cap.required')"
      :data-cap-i18n-error-aria-label="t('admin.login.cap.errorAria')"
      @solve="handleSolve"
      @reset="clearToken"
      @error="clearToken"
    />
  </div>
</template>
