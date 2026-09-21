<template>
  <div v-if="enabled" class="space-y-3 rounded-xl border border-border bg-muted/10 p-4">
    <div>
      <h3 class="text-sm font-semibold">{{ t('personalCenter.security.oidcTitle', { provider: providerName }) }}</h3>
      <p class="mt-1 text-xs text-muted-foreground">{{ bound ? t('personalCenter.security.oidcBound', { username: username || '-' }) : t('personalCenter.security.oidcSubtitle') }}</p>
    </div>
    <Button v-if="!bound" type="button" variant="outline" class="w-full" :disabled="loading" @click="$emit('bind')">
      {{ loading ? t('personalCenter.security.oidcBinding') : t('personalCenter.security.oidcBindButton', { provider: providerName }) }}
    </Button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'

withDefaults(defineProps<{
  enabled?: boolean
  bound?: boolean
  providerName?: string
  username?: string
  loading?: boolean
}>(), {
  enabled: false,
  bound: false,
  providerName: 'OIDC',
  username: '',
  loading: false,
})

defineEmits<{
  bind: []
}>()

const { t } = useI18n()
</script>
