import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { VueQueryPlugin } from '@tanstack/vue-query'
import App from './App.vue'
import router from './router'
import './styles.css'

createApp(App)
  .use(createPinia())
  .use(VueQueryPlugin, { queryClientConfig: { defaultOptions: { queries: { staleTime: 20_000, retry: 1 } } } })
  .use(router)
  .mount('#app')
