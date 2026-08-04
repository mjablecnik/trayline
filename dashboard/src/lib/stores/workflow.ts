import { writable } from 'svelte/store';
import { api, type Workflow } from '$lib/api';

const POLL_INTERVAL_MS = 5000;

export interface WorkflowListState {
	workflows: Workflow[];
	loading: boolean;
	error: string | null;
}

function createWorkflowStore() {
	const { subscribe, set, update } = writable<WorkflowListState>({
		workflows: [],
		loading: false,
		error: null
	});

	let intervalId: ReturnType<typeof setInterval> | null = null;
	let currentProject: string | null = null;

	async function fetchWorkflows(project: string) {
		try {
			const workflows = await api.getWorkflows(project);
			update((s) => ({ ...s, workflows, loading: false, error: null }));
		} catch (err) {
			update((s) => ({
				...s,
				loading: false,
				error: err instanceof Error ? err.message : 'Failed to load workflows'
			}));
		}
	}

	function tick() {
		if (document.hidden || !currentProject) return;
		fetchWorkflows(currentProject);
	}

	return {
		subscribe,
		// Fetches immediately and begins polling every 5 seconds while the tab
		// is visible. Safe to call again to switch projects - replaces any
		// existing interval rather than stacking a second one.
		async start(project: string) {
			currentProject = project;
			set({ workflows: [], loading: true, error: null });
			if (intervalId) clearInterval(intervalId);
			intervalId = setInterval(tick, POLL_INTERVAL_MS);
			await fetchWorkflows(project);
		},
		async refresh() {
			if (!currentProject) return;
			update((s) => ({ ...s, loading: true, error: null }));
			await fetchWorkflows(currentProject);
		},
		stop() {
			if (intervalId) {
				clearInterval(intervalId);
				intervalId = null;
			}
			currentProject = null;
		}
	};
}

export const workflowStore = createWorkflowStore();
