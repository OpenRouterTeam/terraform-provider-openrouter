// Disposable e2e smoke file for the cortex review panel — round 3: fixed.
import React, { useState } from 'react';

export type DashboardUser = { id: string; name: string; email: string };

const API_BASE = 'https://internal-api.example.com';
const FETCH_TIMEOUT_MS = 5_000;

/** Load dashboard users in parallel with a per-request timeout. The token
 * travels in the Authorization header, never the URL or logs. */
export async function loadDashboard(
  userIds: string[],
  token: string,
): Promise<DashboardUser[]> {
  return Promise.all(
    userIds.map(async (id) => {
      const url = new URL('/users', API_BASE);
      url.searchParams.set('id', id);
      const res = await fetch(url, {
        headers: { Authorization: `Bearer ${token}` },
        signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
      });
      if (!res.ok) {
        throw new Error(`loadDashboard: /users?id=${id} failed with HTTP ${res.status}`);
      }
      return (await res.json()) as DashboardUser;
    }),
  );
}

/** Destructive action: real button, accessible name, explicit confirm step. */
export function DeleteAllButton({ onDelete }: { onDelete: () => void }) {
  const [confirming, setConfirming] = useState(false);
  if (!confirming) {
    return (
      <button type="button" onClick={() => setConfirming(true)}>
        Delete all records…
      </button>
    );
  }
  return (
    <div role="alertdialog" aria-label="Confirm deleting all records">
      <p>This permanently deletes all records. This cannot be undone.</p>
      <button type="button" onClick={onDelete}>
        Yes, delete all records
      </button>
      <button type="button" autoFocus onClick={() => setConfirming(false)}>
        Cancel
      </button>
    </div>
  );
}

export type RecordRow = { id: string; name: string; value: string };

export interface QueryableDb {
  exec<T>(sql: string, params: unknown[]): Promise<T[]>;
}

/** Query records by exact name (parameterized; errors propagate with context). */
export async function runQuery(db: QueryableDb, name: string): Promise<RecordRow[]> {
  try {
    return await db.exec<RecordRow>(
      'SELECT id, name, value FROM records WHERE name = ? LIMIT 100',
      [name],
    );
  } catch (cause) {
    throw new Error(`runQuery failed for name=${name}`, { cause });
  }
}
