// Wire types — kept loose because the responses are NATS server payloads
// that we pass through as-is. Only fields we actually render are typed.

export interface ServerInfo {
  id: string;
  name: string;
  cluster?: string;
  ver?: string;
  jetstream?: boolean;
  host?: string;
  flags?: number;
  seq?: number;
  time?: string;
}

export interface MetaCluster {
  name?: string;
  leader?: string;
  cluster_size?: number;
  pending?: number;
}

export interface StatsZ {
  connections?: number;
  total_connections?: number;
  num_subscriptions?: number;
  subscriptions?: number;
  in_msgs?: number;
  out_msgs?: number;
  in_bytes?: number;
  out_bytes?: number;
  slow_consumers?: number;
  cpu?: number;
  mem?: number;
  cores?: number;
  gomaxprocs?: number;
  active_accounts?: number;
  active_servers?: number;
  jetstream?: {
    config?: Record<string, unknown>;
    meta?: MetaCluster;
    stats?: Record<string, unknown>;
  };
}

export interface PingReply {
  server: ServerInfo;
  statsz?: StatsZ;
  data?: Record<string, unknown>;
}

export interface VarzReply {
  server_name?: string;
  server_id?: string;
  version?: string;
  go?: string;
  git_commit?: string;
  uptime?: string;
  host?: string;
  port?: number;
  max_payload?: number;
  max_connections?: number;
  connections?: number;
  total_connections?: number;
  routes?: number;
  leafnodes?: number;
  in_msgs?: number;
  out_msgs?: number;
  in_bytes?: number;
  out_bytes?: number;
  slow_consumers?: number;
  cpu?: number;
  mem?: number;
  config_load_time?: string;
  jetstream?: { config?: Record<string, unknown> };
}

export interface ConnzReply {
  num_connections?: number;
  total?: number;
  offset?: number;
  connections?: Conn[];
}
export interface Conn {
  cid: number;
  name?: string;
  account?: string;
  ip?: string;
  port?: number;
  subscriptions?: number;
  in_msgs?: number;
  out_msgs?: number;
  idle?: string;
}

export interface JszData {
  memory?: number;
  storage?: number;
  reserved_memory?: number;
  reserved_storage?: number;
  accounts?: number;
  ha_assets?: number;
  api?: { total?: number; errors?: number; level?: number };
  meta_cluster?: MetaCluster;
  account_details?: JszAccount[];
}
export interface JszAccount {
  name: string;
  id?: string;
  memory?: number;
  storage?: number;
  api?: { total?: number; errors?: number; level?: number };
  stream_detail?: JszStream[];
}
export interface JszStream {
  name: string;
  created?: string;
  cluster?: StreamCluster;
  config?: StreamConfig;
  state?: StreamState;
  consumer_detail?: ConsumerDetail[] | null;
}
export interface StreamCluster {
  name?: string;
  raft_group?: string;
  leader?: string;
  replicas?: {
    name: string;
    current: boolean;
    active?: number;
    peer?: string;
  }[];
}
export interface StreamConfig {
  name: string;
  subjects?: string[];
  storage?: string;
  retention?: string;
  discard?: string;
  num_replicas?: number;
  max_msgs?: number;
  max_bytes?: number;
  max_age?: number;
  max_msg_size?: number;
  [key: string]: unknown;
}
export interface StreamState {
  messages?: number;
  bytes?: number;
  first_seq?: number;
  last_seq?: number;
  first_ts?: string;
  last_ts?: string;
  consumer_count?: number;
}
export interface ConsumerDetail {
  name?: string;
  config?: ConsumerConfig;
  created?: string;
  cluster?: { leader?: string };
  num_pending?: number;
  num_ack_pending?: number;
  num_redelivered?: number;
  num_waiting?: number;
  delivered?: { consumer_seq?: number; stream_seq?: number };
  ack_floor?: { consumer_seq?: number; stream_seq?: number };
}
export interface ConsumerConfig {
  durable_name?: string;
  name?: string;
  deliver_subject?: string;
  filter_subject?: string;
  filter_subjects?: string[];
  ack_policy?: string;
  max_deliver?: number;
}

export interface HealthzReply {
  status: string;
  error?: string;
  statusz?: Record<string, unknown>;
}

export interface ApiErrorBody {
  message: string;
}

export interface JsApiError {
  error?: {
    code: number;
    err_code: number;
    description?: string;
  };
}

// Cluster-wide JS aggregation (computed client-side from overview replies).
export interface AggregatedOverview {
  cluster?: string;
  meta?: MetaCluster;
  accountList: AggregatedAccount[];
  totalAccounts: number;
  totalStreams: number;
  totalConsumers: number;
  totalMessages: number;
  totalBytes: number;
}
export interface AggregatedAccount {
  name: string;
  id?: string;
  api?: { total?: number; errors?: number };
  streams: AggregatedStream[];
  totals: {
    streams: number;
    consumers: number;
    messages: number;
    bytes: number;
  };
}
export interface AggregatedStream {
  name: string;
  account: string;
  created?: string;
  config?: StreamConfig;
  state?: StreamState;
  cluster?: StreamCluster;
  consumers: AggregatedConsumer[];
}
export interface AggregatedConsumer {
  name?: string;
  stream: string;
  account: string;
  config?: ConsumerConfig;
  created?: string;
  cluster?: {
    leader?: string;
    replicas?: { name: string; current: boolean }[];
  };
  num_pending?: number;
  num_ack_pending?: number;
  num_redelivered?: number;
  num_waiting?: number;
  delivered?: { consumer_seq?: number; stream_seq?: number };
}
