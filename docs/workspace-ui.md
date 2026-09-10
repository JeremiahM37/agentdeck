# Workspace layout

Desktop uses a fixed sidebar; phones keep Board, Sessions, Terminals and More
within thumb reach. More contains Deck, Approvals and Settings, and shows a
pending-approval badge. All views remain deep-linkable.

Sessions use a searchable grid. Attach and Chat are primary; More contains
native attachment, handoff, interruption, project assignment and removal.
Opening More or editing a field prevents periodic refresh from replacing the
focused control. Stop tracking releases an adopted process; Find running agents
can discover and track it again.

Settings separates Targets, Projects, Notifications, and Usage & about.
Switching sections preserves drafts. Arrow keys, Home and End navigate the
section tabs. Task creation stays on the Board; session creation stays in
Sessions. Attach opens an internal terminal tab, and Pop out remains available.

Open in terminal uses the device's default terminal. Linux and Windows setup
instructions live under Tools → Terminal connection setup. The attached terminal and a native terminal can remain open
simultaneously.

Verified layouts: 390px phone and 1440px desktop, live session data, project
forms, secondary menus, terminal controls, keyboard navigation and draft
retention. Regression flows live in e2e/test_workspace_ui.py, alongside the
existing task, routine, approval, terminal-tab and scrolling suites.
