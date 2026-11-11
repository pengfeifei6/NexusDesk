import { createRouter, createWebHistory } from 'vue-router'
import LoginView from '../views/LoginView.vue'
import RegisterView from '../views/RegisterView.vue'
import KnowledgeListView from '../views/KnowledgeListView.vue'
import KnowledgeDetailView from '../views/KnowledgeDetailView.vue'
import KnowledgeSearchView from '../views/KnowledgeSearchView.vue'
import DeskView from '../views/DeskView.vue'
import TicketListView from '../views/TicketListView.vue'
import TicketDetailView from '../views/TicketDetailView.vue'
export const routes = [
  { path: '/', redirect: '/desk' },
  { path: '/login', component: LoginView },
  { path: '/register', component: RegisterView },
  { path: '/organizations/:orgID/knowledge', component: KnowledgeListView },
  { path: '/organizations/:orgID/knowledge/search', component: KnowledgeSearchView },
  { path: '/organizations/:orgID/knowledge/:documentID', component: KnowledgeDetailView },
  { path: '/desk', component: DeskView },
  { path: '/organizations/:orgID/tickets', component: TicketListView },
  { path: '/organizations/:orgID/tickets/:ticketID', component: TicketDetailView },
  { path: '/:pathMatch(.*)*', redirect: '/desk' },
]
export default createRouter({ history: createWebHistory(), routes })
