export interface User {
  id: string
  display_name: string
  email: string
}
export interface Organization {
  id: string
  name: string
  my_role: 'admin' | 'member'
}
export interface Ticket {
  id: string
  org_id: string
  creator_id: string
  assignee_id: string | null
  title: string
  body: string
  priority: number
  status: 'OPEN' | 'IN_PROGRESS' | 'RESOLVED' | 'CLOSED'
  version: string
  created_at: string
  updated_at: string
}
export interface Comment {
  id: string
  ticket_id: string
  author_id: string
  content: string
  created_at: string
}
export interface TicketEvent {
  id: string
  ticket_id: string
  actor_id: string
  event_type: string
  ticket_version: string
  before_data: unknown
  after_data: unknown
  created_at: string
}
export interface KnowledgeDocument {
  id: string
  org_id: string
  uploaded_by: string
  original_filename: string
  media_type: string
  size_bytes: number
  sha256_hex: string
  status: 'UPLOADED' | 'PROCESSING' | 'READY' | 'FAILED'
  failure_code: string | null
  extracted_chars: number
  created_at: string
  updated_at: string
}
export interface DocumentText {
  document_id: string
  content_text: string
  content_sha256: string
  extractor_version: string
  extracted_at: string
}
export interface IndexInfo {
  document_id: string
  status: 'ABSENT' | 'INDEXING' | 'READY' | 'FAILED'
  index_version: string
  embedding_model: string
  embedding_dimension: number
  chunker_version: string
  source_content_sha256: string
  chunk_count: number
  failure_code: string | null
  started_at: string | null
  indexed_at: string | null
  updated_at: string | null
}
export interface RAGSource {
  document_id: string
  chunk_id: string
  chunk_index: number
  source_name: string
  score: number
  fusion_score?: number
  excerpt: string
  start_rune: number
  end_rune: number
}
export type RetrievalMode = 'vector' | 'hybrid'
export interface RAGUsage {
  known: boolean
  model_called: boolean
  prompt_tokens: number | null
  completion_tokens: number | null
  total_tokens: number | null
  cache_hit_tokens: number | null
  cache_miss_tokens: number | null
}
export interface RAGTimings {
  embedding_ms: number
  search_ms: number
  generation_ms: number
  total_ms: number
}
export interface SearchResponse {
  retrieval_mode: RetrievalMode
  items: RAGSource[]
  embedding_model: string
  embedding_ms: number
  search_ms: number
}
export interface AskResponse {
  retrieval_mode: RetrievalMode
  usage: RAGUsage
  timings: RAGTimings
  answer: string
  sources: RAGSource[]
  request_id: string
  insufficient_evidence: boolean
  embedding_model: string
  generation_model: string
}

export interface KnowledgeJob {
  id: string
  org_id: string
  document_id: string
  requested_by: string
  actor_user_id: string
  run_version: string
  status: 'PENDING' | 'RUNNING' | 'RETRYING' | 'SUCCEEDED' | 'FAILED' | 'CANCELED'
  phase: 'WAITING' | 'EXTRACTING' | 'INDEXING' | 'DONE'
  attempts: number
  max_attempts: number
  total_attempts: number
  failure_code: string | null
  retryable: boolean
  last_dispatch_error: string | null
  source_sha256: string
  embedding_model: string
  chunker_version: string
  created_at: string
  updated_at: string
  started_at: string | null
  completed_at: string | null
  next_run_at: string
}

export interface AIConversation {
  id: string
  org_id: string
  owner_user_id: string
  title: string
  status: 'ACTIVE'
  created_at: string
  updated_at: string
}
export interface AIMessage {
  id: string
  conversation_id: string
  client_request_id: string
  role: 'USER' | 'ASSISTANT'
  content: string
  status: 'STREAMING' | 'COMPLETED' | 'FAILED' | 'INTERRUPTED'
  model_name: string
  org_id: string
  citations: RAGSource[]
  input_tokens: number
  output_tokens: number
  created_at: string
  completed_at: string | null
}
export interface AIToolAction {
  id: string
  conversation_id: string
  assistant_message_id: string
  tool_name: 'propose_create_ticket'
  status: 'PROPOSED' | 'EXECUTED' | 'REJECTED' | 'EXPIRED'
  org_id: string
  proposed_by_user_id: string
  arguments: { title: string; body: string; priority: 1 | 2 | 3 }
  expires_at: string
  created_at: string
  confirmed_by_user_id: string | null
  ticket_id: string | null
  confirmed_at: string | null
}

export interface Member {
  user_id: string
  display_name: string
  role: 'admin' | 'member'
  status: 'active' | 'disabled'
  created_at: string
}
