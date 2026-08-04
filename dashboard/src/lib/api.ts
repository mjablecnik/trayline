import { browser } from '$app/environment';
import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import { getToken } from '$lib/auth';
import { auth } from '$lib/stores/auth';

const BASE_URL = import.meta.env.PUBLIC_API_URL as string;

export class ApiError extends Error {
	status: number;
	code: string;

	constructor(status: number, code: string, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
	}
}

export class AuthError extends Error {
	constructor() {
		super('Unauthorized');
		this.name = 'AuthError';
	}
}

export class ConnectionError extends Error {
	constructor(cause?: unknown) {
		super('Could not connect to the server');
		this.name = 'ConnectionError';
		this.cause = cause;
	}
}

function encodePath(path: string): string {
	return path
		.split('/')
		.filter((segment) => segment.length > 0)
		.map(encodeURIComponent)
		.join('/');
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const token = getToken();

	let res: Response;
	try {
		res = await fetch(`${BASE_URL}${path}`, {
			method,
			headers: {
				...(token ? { Authorization: `Bearer ${token}` } : {}),
				'Content-Type': 'application/json'
			},
			body: body !== undefined ? JSON.stringify(body) : undefined
		});
	} catch (cause) {
		throw new ConnectionError(cause);
	}

	if (res.status === 401) {
		auth.logout();
		if (browser) await goto(resolve('/'));
		throw new AuthError();
	}

	if (!res.ok) {
		const err = await res.json().catch(() => ({}) as { error?: string; message?: string });
		throw new ApiError(res.status, err.error ?? 'unknown_error', err.message ?? res.statusText);
	}

	if (res.status === 204) return undefined as T;
	return (await res.json()) as T;
}

export interface CommitSummary {
	hash: string;
	message: string;
	author: string;
	date: string;
}

export interface Project {
	name: string;
	path: string;
	last_commit: CommitSummary | null;
	branch: string;
	has_uncommitted_changes: boolean;
}

export interface ProjectDetail {
	name: string;
	branch: string;
	branches: string[];
	remote_url: string;
	last_commit: CommitSummary | null;
}

export interface TreeFileEntry {
	name: string;
	type: 'file';
	size: number;
}

export interface TreeDirectoryEntry {
	name: string;
	type: 'directory';
}

export type TreeEntry = TreeFileEntry | TreeDirectoryEntry;

export interface TreeResponse {
	type: 'directory';
	path: string;
	entries: TreeEntry[];
}

export interface BlobResponse {
	type: 'file';
	path: string;
	size: number;
	content: string | null;
	language?: string;
	truncated?: boolean;
	binary?: boolean;
}

export interface CommitLogEntry extends CommitSummary {
	short_hash: string;
}

export interface CommitsResponse {
	commits: CommitLogEntry[];
	total: number;
	has_more: boolean;
}

export interface CommitDetail extends CommitLogEntry {
	files_changed: number;
	insertions: number;
	deletions: number;
	diff: string;
}

export type StatusFileState = 'modified' | 'added' | 'untracked' | 'deleted';

export interface StatusFile {
	path: string;
	status: StatusFileState;
	insertions: number;
	deletions: number;
	diff: string | null;
}

export interface StatusResponse {
	clean: boolean;
	files: StatusFile[];
	summary: {
		files_changed: number;
		insertions: number;
		deletions: number;
	};
}

export interface EnvVariable {
	key: string;
	value: string;
}

export interface EnvFile {
	path: string;
	variables: EnvVariable[];
}

export interface EnvResponse {
	files: EnvFile[];
}

export interface PutEnvRequest {
	path: string;
	variables: EnvVariable[];
}

export interface AgentSession {
	session_id: string;
	agent: string;
	model?: string;
	project?: string;
	created_at: string;
	last_message_at: string;
}

export interface StarterPrompt {
	filename: string;
	display_name: string;
	content: string;
}

export interface AssistantFileEntry {
	name: string;
	type: 'file' | 'directory';
	size: number;
}

export interface AssistantDirectoryResponse {
	path: string;
	entries: AssistantFileEntry[];
}

export interface AssistantFileContentResponse {
	path: string;
	filename: string;
	size: number;
	content: string | null;
	truncated: boolean;
}

export interface AssistantSession {
	session_id: string;
	agent: string;
	model?: string;
	is_assistant: boolean;
	created_at: string;
	last_message_at: string;
}

export interface GitCommitEntry {
	hash: string;
	short_hash: string;
	message: string;
	date: string;
}

export interface GitStatusFile {
	path: string;
	status: string;
}

export interface GitStatusResponse {
	clean: boolean;
	files: GitStatusFile[];
	summary: {
		files_changed: number;
		insertions: number;
		deletions: number;
	};
}

export function buildAssistantWsUrl(agent: string, model?: string, sessionId?: string): string {
	const base = (import.meta.env.PUBLIC_API_URL as string).replace(/^http/, 'ws');
	if (sessionId) {
		return `${base}/assistant/chat/${encodeURIComponent(sessionId)}`;
	}
	const params = new URLSearchParams({ agent });
	if (model) params.set('model', model);
	return `${base}/assistant/chat?${params}`;
}

export function buildWsUrl(
	projectName: string,
	agent: string,
	model?: string,
	sessionId?: string
): string {
	const base = (import.meta.env.PUBLIC_API_URL as string).replace(/^http/, 'ws');
	const encoded = encodeURIComponent(projectName);
	if (sessionId) {
		return `${base}/projects/${encoded}/chat/${encodeURIComponent(sessionId)}`;
	}
	const params = new URLSearchParams({ agent });
	if (model) params.set('model', model);
	return `${base}/projects/${encoded}/chat?${params}`;
}

export type PipelineType = 'tasks' | 'processes' | 'workflows';

export interface Pipeline {
	name: string;
	type: PipelineType;
	display_name: string;
}

export interface PipelinesResponse {
	tasks: Pipeline[];
	processes: Pipeline[];
	workflows: Pipeline[];
}

export interface PipelineDetail {
	name: string;
	type: string;
	variables: Record<string, string>;
}

export interface Spec {
	name: string;
	created_at: string;
}

export type WorkflowStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled';

export interface Workflow {
	id: string;
	pipeline: string;
	variables: Record<string, string>;
	status: WorkflowStatus;
	created_at: string;
	started_at?: string;
	completed_at?: string;
	error?: string;
	exit_code?: number;
	log?: string;
	truncated?: boolean;
}

export interface ScheduleWorkflowRequest {
	pipeline: string;
	variables: Record<string, string>;
}

export interface EditWorkflowRequest {
	pipeline?: string;
	variables: Record<string, string>;
}

export function buildWorkflowLogWsUrl(projectName: string, workflowId: string): string {
	const base = (import.meta.env.PUBLIC_API_URL as string).replace(/^http/, 'ws');
	return `${base}/projects/${encodeURIComponent(projectName)}/workflows/${encodeURIComponent(workflowId)}/logs`;
}

export const api = {
	getProjects: () => request<Project[]>('GET', '/projects'),

	getProject: (name: string) =>
		request<ProjectDetail>('GET', `/projects/${encodeURIComponent(name)}`),

	getTree: (name: string, ref: string, path: string) =>
		request<TreeResponse>(
			'GET',
			`/projects/${encodeURIComponent(name)}/tree/${encodeURIComponent(ref)}/${encodePath(path)}`
		),

	getBlob: (name: string, ref: string, path: string) =>
		request<BlobResponse>(
			'GET',
			`/projects/${encodeURIComponent(name)}/blob/${encodeURIComponent(ref)}/${encodePath(path)}`
		),

	getCommits: (name: string, ref: string, limit = 50, offset = 0) =>
		request<CommitsResponse>(
			'GET',
			`/projects/${encodeURIComponent(name)}/commits?${new URLSearchParams({
				ref,
				limit: String(limit),
				offset: String(offset)
			})}`
		),

	getCommit: (name: string, hash: string) =>
		request<CommitDetail>(
			'GET',
			`/projects/${encodeURIComponent(name)}/commits/${encodeURIComponent(hash)}`
		),

	getStatus: (name: string) =>
		request<StatusResponse>('GET', `/projects/${encodeURIComponent(name)}/status`),

	discardFile: (name: string, path: string) =>
		request<void>('POST', `/projects/${encodeURIComponent(name)}/changes/discard`, { path }),

	discardAllChanges: (name: string) =>
		request<void>('POST', `/projects/${encodeURIComponent(name)}/changes/discard-all`),

	getEnv: (name: string) =>
		request<EnvResponse>('GET', `/projects/${encodeURIComponent(name)}/env`),

	putEnv: (name: string, data: PutEnvRequest) =>
		request<EnvFile>('PUT', `/projects/${encodeURIComponent(name)}/env`, data),

	getProjectSessions: (name: string) =>
		request<AgentSession[]>('GET', `/projects/${encodeURIComponent(name)}/sessions`),

	terminateProjectSession: (name: string, sessionId: string) =>
		request<{ session_id: string; status: string }>(
			'POST',
			`/projects/${encodeURIComponent(name)}/sessions/${encodeURIComponent(sessionId)}/terminate`
		),

	getSessions: () => request<AgentSession[]>('GET', '/sessions'),

	terminateSession: (sessionId: string) =>
		request<{ session_id: string; status: string }>(
			'POST',
			`/sessions/${encodeURIComponent(sessionId)}/terminate`
		),

	getPipelines: (name: string) =>
		request<PipelinesResponse>('GET', `/projects/${encodeURIComponent(name)}/pipelines`),

	getPipelineDetail: (name: string, type: string, pipeline: string) =>
		request<PipelineDetail>(
			'GET',
			`/projects/${encodeURIComponent(name)}/pipelines/${encodeURIComponent(type)}/${encodeURIComponent(pipeline)}`
		),

	getSpecs: (name: string) => request<Spec[]>('GET', `/projects/${encodeURIComponent(name)}/specs`),

	getWorkflows: (name: string) =>
		request<Workflow[]>('GET', `/projects/${encodeURIComponent(name)}/workflows`),

	getWorkflow: (name: string, id: string) =>
		request<Workflow>(
			'GET',
			`/projects/${encodeURIComponent(name)}/workflows/${encodeURIComponent(id)}`
		),

	createWorkflow: (name: string, data: ScheduleWorkflowRequest) =>
		request<Workflow>('POST', `/projects/${encodeURIComponent(name)}/workflows`, data),

	updateWorkflow: (name: string, id: string, data: EditWorkflowRequest) =>
		request<Workflow>(
			'PUT',
			`/projects/${encodeURIComponent(name)}/workflows/${encodeURIComponent(id)}`,
			data
		),

	cancelWorkflow: (name: string, id: string) =>
		request<Workflow>(
			'DELETE',
			`/projects/${encodeURIComponent(name)}/workflows/${encodeURIComponent(id)}`
		),

	getAssistantSessions: () => request<AssistantSession[]>('GET', '/assistant/sessions'),

	terminateAssistantSession: (sessionId: string) =>
		request<{ session_id: string; status: string }>(
			'POST',
			`/assistant/sessions/${encodeURIComponent(sessionId)}/terminate`
		),

	getAssistantPrompts: () => request<StarterPrompt[]>('GET', '/assistant/prompts'),

	getAssistantPrompt: (filename: string) =>
		request<StarterPrompt>('GET', `/assistant/prompts/${encodeURIComponent(filename)}`),

	putAssistantPrompt: (filename: string, content: string) =>
		request<{ status: string }>('PUT', `/assistant/prompts/${encodeURIComponent(filename)}`, {
			content
		}),

	deleteAssistantPrompt: (filename: string) =>
		request<{ status: string }>('DELETE', `/assistant/prompts/${encodeURIComponent(filename)}`),

	getAssistantFiles: (path?: string) =>
		request<AssistantDirectoryResponse | AssistantFileContentResponse>(
			'GET',
			`/assistant/files${path ? '/' + encodePath(path) : ''}`
		),

	getAssistantFileCommits: (limit = 20, offset = 0) =>
		request<GitCommitEntry[]>('GET', `/assistant/files/commits?limit=${limit}&offset=${offset}`),

	getAssistantFileStatus: () => request<GitStatusResponse>('GET', '/assistant/files/status'),

	getAssistantSummary: () =>
		request<AssistantFileContentResponse>('GET', '/assistant/files/summary.md')
};
