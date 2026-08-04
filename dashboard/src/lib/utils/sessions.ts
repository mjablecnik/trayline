import type { AgentSession } from '$lib/api';

export interface SessionGroup {
	project: string | null;
	sessions: AgentSession[];
}

/**
 * Groups sessions by project, preserving the input order both across groups
 * (so the project with the most recently active session — the API already
 * sorts sessions that way — comes first) and within each group. Sessions
 * with no project (e.g. started outside the dashboard) land in a `null` group.
 */
export function groupSessionsByProject(sessions: AgentSession[]): SessionGroup[] {
	const groups: SessionGroup[] = [];
	const byProject = new Map<string | null, SessionGroup>();

	for (const session of sessions) {
		const key = session.project ?? null;
		let group = byProject.get(key);
		if (!group) {
			group = { project: key, sessions: [] };
			byProject.set(key, group);
			groups.push(group);
		}
		group.sessions.push(session);
	}

	return groups;
}
