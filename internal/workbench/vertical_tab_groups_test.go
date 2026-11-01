package workbench

import (
	"reflect"
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// Restored, disconnected tabs exercise grouping without a network substitute.
func restoredGroupedTabs(t *testing.T, entries ...savedSession) *State {
	t.Helper()
	state := newTestState(t, Options{})
	sessions := state.restoreSessions(entries, len(entries))
	state.deck = layoutcore.Deck{}
	for index, session := range sessions {
		id, pane, err := state.deck.Create(entries[index].Label)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.deck.Bind(id, pane, layoutcore.SessionID(session)); err != nil {
			t.Fatal(err)
		}
	}
	state.syncActiveSession()
	state.activeSection = sectionTerminal
	state.rebuildTabOptions()
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsGrouping(true)
	return state
}

func groupingEntries() []savedSession {
	return []savedSession{
		{Label: "Server A", Address: "a.example.com", User: "admin", Port: 22},
		{Label: "Server B", Address: "b.example.com", User: "admin", Port: 22},
		{Label: "Server A logs", Address: "A.EXAMPLE.COM", User: "admin", Port: 22},
		{Label: "Server A other account", Address: "a.example.com", User: "deploy", Port: 22},
	}
}

func TestVerticalTabGroupingIsAnIndependentLocalSetting(t *testing.T) {
	state := newTestState(t, Options{})
	if state.verticalTabs.grouping {
		t.Fatal("grouping must default to off")
	}
	state.Click(hitSettings)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 1800})
	if _, ok := hitByID(&canvas, hitSettingsVerticalTabsGrouping); !ok {
		t.Fatal("General settings must expose the grouping switch")
	}
	state.Click(hitSettingsVerticalTabsGrouping)
	if !state.verticalTabs.grouping || state.tabOrientation != tabsHorizontal || state.verticalTabsGrouped() {
		t.Fatal("enabling grouping must not enable vertical tabs or affect horizontal mode")
	}
	options := state.options
	state.Close()
	again := newTestState(t, options)
	if !again.verticalTabs.grouping || again.tabOrientation != tabsHorizontal {
		t.Fatal("the independent preference did not survive restart")
	}
}

func TestVerticalTabGroupingCollapsesWithoutChangingTheDeck(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	before := tabOrder(state)
	var canvas ui.Canvas
	size := ui.Size{Width: 1200, Height: 760}
	state.Paint(&canvas, size)
	view := &state.verticalTabs.grouped
	if len(view.groups) != 3 || view.groups[0].count != 2 || len(view.rows) != 5 {
		t.Fatalf("expected two Server A connections and two singletons: %+v", view.groups)
	}
	if got := []layoutcore.WorkspaceID{view.order[0].ID, view.order[1].ID, view.order[2].ID, view.order[3].ID}; !reflect.DeepEqual(got, []layoutcore.WorkspaceID{before[0], before[2], before[1], before[3]}) {
		t.Fatalf("grouping did not bring nonadjacent Server A tabs together: %v", got)
	}
	header := view.groups[0].id(state.workspaceTabs)
	hit, ok := hitByID(&canvas, header)
	if !ok || !hit.Expandable || !hit.Expanded {
		t.Fatal("the server header must be an accessible disclosure")
	}
	first, _ := hitByID(&canvas, hitTabList.At(int(before[0])))
	third, _ := hitByID(&canvas, hitTabList.At(int(before[2])))
	if first.Bounds.X <= hit.Bounds.X || third.Bounds.Y <= first.Bounds.Y {
		t.Fatal("group members must be indented below their server")
	}
	state.Click(header)
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitTabList.At(int(before[2]))); ok {
		t.Fatal("collapsed children must not retain invisible click targets")
	}
	if !reflect.DeepEqual(tabOrder(state), before) || len(state.sessions) != 4 {
		t.Fatal("collapsing must not reorder the deck or close connections")
	}
	state.setVerticalTabsAutoHide(true)
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitTabList.At(int(before[2]))); !ok {
		t.Fatal("the compact icon rail must keep every connection reachable")
	}
	state.setVerticalTabsAutoHide(false)
	state.activateWorkspace(before[2])
	canvas.Reset()
	state.Paint(&canvas, size)
	if hit, ok := hitByID(&canvas, hitTabList.At(int(before[2]))); !ok || !hit.Selected {
		t.Fatal("activating a hidden connection must expand and reveal it")
	}
	state.closeWorkspace(before[0])
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitVerticalTabGroups.At(int(before[2]))); ok {
		t.Fatal("a server with one remaining connection must lose its group header")
	}
	state.setTabOrientation(tabsHorizontal)
	canvas.Reset()
	state.Paint(&canvas, size)
	if len(state.chrome.bar.tabs) != 3 || state.tabStrip.vertical {
		t.Fatal("horizontal mode must ignore the grouping setting")
	}
}

func TestVerticalTabGroupingNavigationReorderAndCloseFollowVisualOrder(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	ids := tabOrder(state)
	var canvas ui.Canvas
	size := ui.Size{Width: 1200, Height: 760}
	state.Paint(&canvas, size)
	header := state.verticalTabs.grouped.groups[0].id(state.workspaceTabs)
	result := state.Key(ui.KeyEvent{Target: hitTabList.At(int(ids[0])), Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitTabList.At(int(ids[2])) {
		t.Fatal("Down must follow grouped visual order, not raw deck order")
	}
	state.Key(ui.KeyEvent{Target: header, Key: ui.KeyLeft})
	canvas.Reset()
	state.Paint(&canvas, size)
	result = state.Key(ui.KeyEvent{Target: header, Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitTabList.At(int(ids[1])) {
		t.Fatal("Down must skip the children of a collapsed group")
	}
	state.Key(ui.KeyEvent{Target: header, Key: ui.KeyRight})
	state.activateWorkspace(ids[0])
	state.cycleWorkspace(1)
	if state.deck.ActiveWorkspace() != ids[2] {
		t.Fatal("Ctrl+Tab must follow the grouped order")
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	first, _ := hitByID(&canvas, hitTabList.At(int(ids[0])))
	third, _ := hitByID(&canvas, hitTabList.At(int(ids[2])))
	target := hitTabList.At(int(ids[2]))
	from := ui.Point{X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2}
	to := ui.Point{X: from.X, Y: first.Bounds.Y + 2}
	press(state, target, from)
	dragTo(state, target, to)
	if state.tabDrag.drop.kind != dropReorder || state.tabDrag.drop.boundary != 0 {
		t.Fatalf("group headers must not be counted as deck tabs: %+v", state.tabDrag.drop)
	}
	release(state, target, to)
	if tabOrder(state)[0] != ids[2] {
		t.Fatal("dragging within a server did not reorder its connections")
	}
	state.tabMenu = tabMenu{open: true, workspace: ids[2]}
	state.clickTabMenu(hitTabMenuCloseRight)
	if state.modal != modalCloseTabs || state.tabsClose.count != 3 {
		t.Fatalf("close below must include all following visual tabs: %+v", state.tabsClose)
	}
	state.clickCloseTabsDialog(hitTabsCloseCancel)
	canvas.Reset()
	state.Paint(&canvas, size)
	first, _ = hitByID(&canvas, hitTabList.At(int(ids[2])))
	third, _ = hitByID(&canvas, hitTabList.At(int(ids[0])))
	target = hitTabList.At(int(ids[2]))
	from = ui.Point{X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2}
	to = ui.Point{X: from.X, Y: third.Bounds.Y + third.Bounds.Height - 2}
	press(state, target, from)
	dragTo(state, target, to)
	release(state, target, to)
	if got := tabOrder(state); !reflect.DeepEqual(got, ids) {
		t.Fatalf("reordering a child must preserve other servers' slots: %v", got)
	}
	state.tabMenu = tabMenu{open: true, workspace: ids[2]}
	state.clickTabMenu(hitTabMenuCloseRight)
	if state.tabsClose.count != 2 || !reflect.DeepEqual(state.tabsClose.ids, []layoutcore.WorkspaceID{ids[1], ids[3]}) {
		t.Fatalf("close below must include the earlier deck tab displayed below the group: %+v", state.tabsClose)
	}
}

func TestVerticalTabGroupingDistinguishesTargetsAndMixedSplits(t *testing.T) {
	entries := groupingEntries()
	entries = append(entries,
		savedSession{Label: "Other port", Address: "a.example.com", User: "admin", Port: 2222},
		savedSession{Label: "VNC", Address: "a.example.com", User: "admin", Port: 22, savedTransport: savedTransport{Protocol: "vnc"}},
		savedSession{Label: "Local", Local: true},
		savedSession{Label: "Local 2", Local: true},
		savedSession{Label: "Another shell", Local: true, LocalShell: "/bin/bash"},
	)
	state := restoredGroupedTabs(t, entries...)
	state.rebuildVerticalTabGroups(false)
	if len(state.verticalTabs.grouped.groups) != 7 {
		t.Fatalf("port, protocol, account and shell must remain distinct: %+v", state.verticalTabs.grouped.groups)
	}
	tabs := state.workspaceTabs
	if _, err := state.deck.Dock(tabs[0].ID, tabs[1].ID, tabs[0].ActivePane, layoutcore.Horizontal, layoutcore.After); err != nil {
		t.Fatal(err)
	}
	state.rebuildTabOptions()
	state.rebuildVerticalTabGroups(false)
	key, _ := state.verticalWorkspaceKey(state.workspaceTabs[0])
	if key.workspace != state.workspaceTabs[0].ID {
		t.Fatal("a split across different servers must remain an independent tab")
	}
}

func TestVerticalTabGroupingRevealsActivatedConnectionsAfterOverflow(t *testing.T) {
	entries := groupingEntries()[:2]
	for range 18 {
		entries = append(entries, savedSession{Label: "Server A shell", Address: "a.example.com", User: "admin", Port: 22})
	}
	state := restoredGroupedTabs(t, entries...)
	size := ui.Size{Width: 1000, Height: 560}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	last := state.deck.ActiveWorkspace()
	hit, ok := hitByID(&canvas, hitTabList.At(int(last)))
	area := state.tabStrip.area
	if !ok || hit.Bounds.Y < area.Y || hit.Bounds.Y+hit.Bounds.Height > area.Y+area.Height+0.5 {
		t.Fatal("active child must scroll into view beneath its server header")
	}
	group := state.verticalTabs.grouped.groups[0]
	state.Click(group.id(state.workspaceTabs))
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.tabStrip.offset > state.tabStrip.max {
		t.Fatal("collapsing must clamp the now shorter list's scroll offset")
	}
	state.activateWorkspace(state.workspaceTabs[0].ID)
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.verticalTabs.grouped.collapsed[group.key] {
		t.Fatal("selecting a child must expand its collapsed server")
	}
}
