import { describe, expect, it } from 'vitest';
import type { AgentSession } from '$lib/api';
import { groupSessionsByProject } from './sessions';

function session(overrides: Partial<AgentSession>): AgentSession {
	return {
		session_id: 'id',
		agent: 'claude',
		created_at: '2026-01-01T00:00:00Z',
		last_message_at: '2026-01-01T00:00:00Z',
		...overrides
	};
}

describe('groupSessionsByProject', () => {
	it('groups sessions under their project, preserving first-seen project order', () => {
		const sessions = [
			session({ session_id: 'a', project: 'proj-a' }),
			session({ session_id: 'b', project: 'proj-b' }),
			session({ session_id: 'c', project: 'proj-a' })
		];

		const groups = groupSessionsByProject(sessions);

		expect(groups).toEqual([
			{ project: 'proj-a', sessions: [sessions[0], sessions[2]] },
			{ project: 'proj-b', sessions: [sessions[1]] }
		]);
	});

	it('buckets sessions with no project under a null group', () => {
		const sessions = [
			session({ session_id: 'a' }),
			session({ session_id: 'b', project: 'proj-a' })
		];

		const groups = groupSessionsByProject(sessions);

		expect(groups).toEqual([
			{ project: null, sessions: [sessions[0]] },
			{ project: 'proj-a', sessions: [sessions[1]] }
		]);
	});

	it('returns an empty array for no sessions', () => {
		expect(groupSessionsByProject([])).toEqual([]);
	});
});
