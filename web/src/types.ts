export type Source = {
  proxyId?: string;
  hubProxyMode?: "never" | "always";
  probeIntervalMinutes?: number;
  smokeKeyword?: string;
  hubPluginId?: string;
  id: string;
  name: string;
  protocol: string;
  mediaTypes: string[];
  capabilities: string[];
  mode: string;
  trust: string;
  network: string;
  updatePolicy: string;
  intervalMinutes: number;
  url?: string;
  runtimeId?: string;
  catalogId?: string;
  enabled: boolean;
  health: string;
  score: number;
  failures: number;
  activeRevision?: string;
  stagedRevision?: string;
  lastChecked?: string;
  updatedAt: string;
  createdAt: string;
};
export type Item = {
  logo?: string;
  id?: string;
  name: string;
  url?: string;
  group?: string;
  language?: string;
  region?: string;
};
export type Normalized = {
  protocol: string;
  mediaTypes: string[];
  capabilities: string[];
  items: Item[];
  warnings: string[];
  requiresRuntime: boolean;
  config?: unknown;
};
export type Revision = {
  id: string;
  sourceId: string;
  hash: string;
  status: string;
  createdAt: string;
  normalized: Normalized;
  diff: {
    added: number;
    removed: number;
    changed: number;
    domainChanges: string[];
    requiresReview: boolean;
  };
};
export type Probe = {
  id: string;
  level: string;
  success: boolean;
  latencyMs: number;
  code: string;
  checks: string[];
  createdAt: string;
};
export type Member = {
  sourceId: string;
  priority: number;
  role: string;
  weight: number;
  minScore: number;
  mediaTypes: string[];
  languages: string[];
  regions: string[];
  devices: string[];
  networks: string[];
  timeoutMs: number;
  maxConcurrency: number;
};
export type ChannelRule = {
  sourceId?: string;
  match: string;
  name?: string;
  group?: string;
  tvgId?: string;
  logo?: string;
  hide: boolean;
};
export type SourceSet = {
  channelRules?: ChannelRule[];
  autoPublish?: boolean;
  minAvailable?: number;
  maxExcludedPercent?: number;
  id: string;
  name: string;
  description: string;
  members: Member[];
  currentPublication?: string;
  previousPublication?: string;
  updatedAt: string;
};
export type Publication = {
  id: string;
  setId: string;
  revision: string;
  createdAt: string;
  artifacts: Record<
    string,
    { contentType: string; body: string; hash: string }
  >;
  sourceRevisions: Record<string, string>;
  exclusions: Record<string, string>;
};
export type Binding = {
  id: string;
  name: string;
  setId: string;
  formats: string[];
  expiresAt: string;
  revoked: boolean;
  generation: number;
  createdAt: string;
};
export type Runtime = {
  id: string;
  name: string;
  driver: string;
  url: string;
  network: string;
  trust: string;
  health: string;
  capabilities: string[];
  lastChecked?: string;
  lastSync?: string;
  version?: string;
  state?: { itemCount?: number; probeLevel?: string; configSync?: string };
};
export type Catalog = {
  id: string;
  name: string;
  url: string;
  network: string;
  trust: string;
  enabled: boolean;
  intervalMinutes: number;
  lastSync?: string;
};
export type Candidate = {
  id: string;
  name: string;
  url: string;
  catalogId: string;
  protocol?: string;
  status: string;
  sourceId?: string;
  discoveredAt: string;
};
export type Job = {
  id: string;
  kind: string;
  targetId: string;
  status: string;
  attempts: number;
  error?: string;
  createdAt: string;
  finishedAt?: string;
};
export type Audit = {
  id: string;
  action: string;
  targetId: string;
  createdAt: string;
};
export type Description = {
  protocol: string;
  mediaTypes: string[];
  capabilities: string[];
  runtime: boolean;
};
export type Connector = {
  name: string;
  statusPath: string;
  statePath: string;
  requiredKey: string;
  protocols: string[];
  capabilities: string[];
  probeLevel: string;
};
export type Meta = {
  adapters: Description[];
  connectors: Record<string, Connector>;
  formats: string[];
};
export type Data = {
  sources: Source[];
  catalogs: Catalog[];
  candidates: Candidate[];
  sets: SourceSet[];
  publications: Publication[];
  bindings: Binding[];
  runtimes: Runtime[];
  jobs: Job[];
  audits: Audit[];
  meta: Meta;
};
