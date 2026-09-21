import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useUserAuthStore } from '../stores/userAuth'
import { userProfileAPI } from '../api'

/** Shared callback logic for the generic OIDC login and binding flows. */
export function useOIDCCallback() {
  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const userAuthStore = useUserAuthStore()
  const loading = ref(true)
  const errMsg = ref('')

  onMounted(async () => {
    const code = String(route.query.code || '')
    const state = String(route.query.state || '')
    const oauthError = String(route.query.error || '')
    const intent = sessionStorage.getItem('oidc_intent') || 'login'
    const savedRedirect = sessionStorage.getItem('oidc_redirect') || ''
    sessionStorage.removeItem('oidc_intent')
    sessionStorage.removeItem('oidc_redirect')

    if (oauthError || !code || !state) {
      errMsg.value = t('auth.oidcCallback.failed')
      loading.value = false
      return
    }

    try {
      if (intent === 'bind') {
        await userProfileAPI.oidcBindCallback({ code, state })
        await router.replace({ path: '/me/security', query: { oidcBound: '1' } })
        return
      }
      const result = await userAuthStore.oidcLogin({ code, state })
      if (result?.requiresTotp) {
        const query: Record<string, string> = { oidc2fa: '1' }
        if (savedRedirect) query.redirect = savedRedirect
        await router.replace({ path: '/auth/login', query })
        return
      }
      await router.replace(savedRedirect || '/me/orders')
    } catch (err: any) {
      errMsg.value = err?.message || t('auth.oidcCallback.failed')
      loading.value = false
    }
  })

  return { loading, errMsg }
}
