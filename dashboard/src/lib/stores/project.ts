import { writable } from 'svelte/store';
import type { ProjectDetail } from '$lib/api';

interface ProjectState {
	detail: ProjectDetail | null;
	ref: string;
}

function createProjectStore() {
	const { subscribe, set, update } = writable<ProjectState>({ detail: null, ref: '' });

	return {
		subscribe,
		setDetail(detail: ProjectDetail, initialRef?: string) {
			update((s) => ({ detail, ref: s.ref || initialRef || detail.branch }));
		},
		setRef(ref: string) {
			update((s) => ({ ...s, ref }));
		},
		reset() {
			set({ detail: null, ref: '' });
		}
	};
}

export const project = createProjectStore();
