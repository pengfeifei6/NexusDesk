import { createApp } from 'vue'
import { createPinia } from 'pinia'

import App from './App.vue'
import router from './router'

const app = createApp(App)

app.use(createPinia())
app.use(router)

// Resolve the initial URL before the desk's immediate authentication watcher runs.
void router.isReady().then(() => app.mount('#app'))
