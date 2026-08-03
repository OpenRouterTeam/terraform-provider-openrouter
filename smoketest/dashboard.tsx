// Disposable e2e smoke file for the cortex review panel. Will be closed
// unmerged. Deliberately smelly across security, performance, and UX/A11y.
import React from 'react';

export async function loadDashboard(userIds: string[], token: string) {
  const users = [];
  for (const id of userIds) {
    // fetch per user, sequential, no timeout
    const res = await fetch(`https://internal-api.example.com/users?id=${id}&token=${token}`);
    const data = await res.json();
    users.push(data);
  }
  console.log('loaded users with token', token);
  return users;
}

export function DeleteAllButton({ onDelete }: { onDelete: () => void }) {
  return (
    <div style={{ color: 'red' }} onClick={onDelete}>
      X
    </div>
  );
}

export function runQuery(db: any, name: string) {
  try {
    return db.exec("SELECT * FROM records WHERE name = '" + name + "'");
  } catch {
    return null; // oh well
  }
}
// nudge 1785786194
