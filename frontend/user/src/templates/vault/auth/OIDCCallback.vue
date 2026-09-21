<template>
  <div class="min-h-[50vh] flex items-center justify-center px-6 py-12 text-center">
    <Card class="w-full max-w-md">
      <div class="space-y-4 p-6">
        <p v-if="loading" class="text-sm text-muted-foreground">{{ t('auth.oidcCallback.processing') }}</p>
        <template v-else-if="errMsg">
          <AlertCircle class="mx-auto h-8 w-8 text-destructive" />
          <p class="text-sm text-destructive">{{ errMsg }}</p>
          <Button variant="outline" as-child>
            <RouterLink to="/auth/login">{{ t('auth.oidcCallback.backToLogin') }}</RouterLink>
          </Button>
        </template>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { AlertCircle } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { useOIDCCallback } from '../../../composables/useOIDCCallback'

const { t } = useI18n()
const { loading, errMsg } = useOIDCCallback()
</script>
