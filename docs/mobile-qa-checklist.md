# Mobile QA Checklist

Use a real phone browser whenever possible. Focus on portrait mode first, then a quick landscape pass.

## Global shell

1. Open the mobile menu and confirm it can open and close cleanly.
2. Trigger a toast notification and confirm it stays readable above the safe area.
3. Verify background scroll is locked while any modal is open.

## Calendar

1. Open a calendar card.
2. Confirm the detail modal is centered and fully visible.
3. Confirm the summary text loads in the white content section.
4. Open the subscribe modal and verify the action buttons remain visible without horizontal overflow.

## Subscriptions

1. Open the subscription detail modal.
2. Open the execution history modal.
3. Open add, batch-add, search, dashboard, edit, and delete modals.
4. Confirm each modal renders above the page shell and does not get clipped.

## Local library

1. Open a local anime detail panel from the list.
2. Use the diagnostics panel on a narrow viewport.
3. Confirm issue recovery highlights and action buttons remain tappable.

## Settings and backup

1. Switch across tabs on a phone-width viewport.
2. Verify save buttons remain visible and full-width where expected.
3. Open the backup analyze and delete confirmation dialogs.

## Playback

1. Open a playable episode page.
2. Confirm playback diagnostics are readable on mobile.
3. Trigger a playback error path and verify the recovery hint is visible without clipping.
