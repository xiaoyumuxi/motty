package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func TestVerticalTabsSettingsPersistAndSupportKeyboard(t *testing.T) {
	state := newTestState(t, Options{})
	if state.tabOrientation != tabsHorizontal || tabOrientationFromString("invalid") != tabsHorizontal {
		t.Fatal("old or invalid settings must keep horizontal tabs")
	}
	state.Click(hitSettings)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 1800})
	for _, id := range []ui.ID{hitSettingsTabsHorizontal, hitSettingsTabsVertical, hitSettingsVerticalTabsAutoHide} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("missing General setting %v", id)
		}
	}
	result := state.Key(ui.KeyEvent{Target: hitSettingsTabsHorizontal, Key: ui.KeyRight})
	if !result.Handled || result.Focus != hitSettingsTabsVertical || state.tabOrientation != tabsVertical {
		t.Fatalf("keyboard choice: %+v, orientation %v", result, state.tabOrientation)
	}
	state.Click(hitSettingsVerticalTabsAutoHide)
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.TabOrientation != "vertical" || !saved.VerticalTabsAutoHide {
		t.Fatalf("saved tabs: %+v, %v", saved, err)
	}
	options := state.options
	state.Close()
	restored := newTestState(t, options)
	if restored.tabOrientation != tabsVertical || !restored.verticalTabs.autoHide || !restored.tabStrip.vertical {
		t.Fatal("tab preferences did not survive restart")
	}
}

func TestVerticalTabsFitWorkspaceAndRemainReachableFromSettings(t *testing.T) {
	state := newTestState(t, Options{})
	for range 3 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 700, Height: 560}} {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		frame := state.workspacePixelBounds(size)
		if frame.X != verticalTabsWidth || frame.Width+frame.X != size.Width {
			t.Fatalf("workspace does not make room for tabs: %+v", frame)
		}
		var previous ui.Rect
		for _, tab := range state.workspaceTabs {
			hit, ok := hitByID(&canvas, hitTabList.At(int(tab.ID)))
			if !ok || hit.Bounds.Y < windowBarHeight || hit.Bounds.X+hit.Bounds.Width > frame.X || hit.Bounds.Y < previous.Y+previous.Height {
				t.Fatalf("tab %d is not stacked in the sidebar: %+v", tab.ID, hit)
			}
			previous = hit.Bounds
		}
		close, ok := hitByID(&canvas, hitTabCloseList.At(int(state.deck.ActiveWorkspace())))
		if !ok || close.Bounds.X+close.Bounds.Width > frame.X {
			t.Fatal("active tab lost its close button")
		}
		plus, ok := hitByID(&canvas, hitNewTab)
		if !ok || plus.Bounds.X+plus.Bounds.Width > size.Width {
			t.Fatal("titlebar new-tab button is offscreen")
		}
	}
	state.Click(hitSettings)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	first := state.workspaceTabs[0].ID
	if hit, ok := hitByID(&canvas, hitTabList.At(int(first))); !ok || hit.Bounds.X+hit.Bounds.Width > navigationWidth {
		t.Fatal("settings must retain the session list in its navigation sidebar")
	}
	state.Click(hitTabList.At(int(first)))
	if !state.terminalPage() || state.deck.ActiveWorkspace() != first {
		t.Fatal("sidebar tab did not return to the workspace")
	}
	state.Click(hitTabCloseList.At(int(first)))
	if len(state.deck.Workspaces(nil)) != 2 {
		t.Fatal("vertical tab close did not close the workspace")
	}
}

func TestVerticalTabsScrollNavigateAndReorder(t *testing.T) {
	state := newTestState(t, Options{})
	for range 20 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	size := ui.Size{Width: 1000, Height: 600}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if state.tabStrip.max <= 0 || state.tabStrip.offset != state.tabStrip.max {
		t.Fatal("active tab must be revealed at the end of an overflowing sidebar")
	}
	area := state.tabStrip.area
	result := state.Scroll(ui.ScrollEvent{X: area.X + 10, Y: area.Y + 10, DeltaY: -10000, Viewport: size})
	if !result.Handled || state.tabStrip.offset != 0 {
		t.Fatal("wheel did not scroll the sidebar to the start")
	}
	first, second := state.workspaceTabs[0].ID, state.workspaceTabs[1].ID
	state.Click(hitTabList.At(int(first)))
	canvas.Reset()
	state.Paint(&canvas, size)
	result = state.Key(ui.KeyEvent{Target: hitTabList.At(int(first)), Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitTabList.At(int(second)) {
		t.Fatalf("vertical tabs must navigate with Up/Down: %+v", result)
	}
	hit, _ := hitByID(&canvas, hitTabList.At(int(first)))
	from := ui.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
	to := ui.Point{X: from.X, Y: state.tabStrip.origin + 3*state.tabStrip.pitch}
	target := hitTabList.At(int(first))
	press(state, target, from)
	dragTo(state, target, to)
	if state.tabDrag.drop.kind != dropReorder || state.tabDrag.drop.zone.Height != 2 {
		t.Fatalf("vertical drop needs a horizontal insertion marker: %+v", state.tabDrag.drop)
	}
	release(state, target, to)
	if got := tabOrder(state); got[0] != second || got[2] != first {
		t.Fatalf("vertical drag did not reorder: %v", got)
	}
}

func TestVerticalTabsHoverDoesNotResizeWorkspace(t *testing.T) {
	state := newTestState(t, Options{})
	openEmptyTab(state)
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	size := ui.Size{Width: 1000, Height: 600}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	before := state.workspacePixelBounds(size)
	if state.verticalTabs.bounds.Width != verticalTabsRail || before.X != verticalTabsRail {
		t.Fatal("auto-hide must start as a compact rail")
	}
	result := state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, X: 20, Y: 100, Viewport: size})
	canvas.Reset()
	state.Paint(&canvas, size)
	if !result.Repaint || state.verticalTabs.bounds.Width != verticalTabsWidth || state.workspacePixelBounds(size) != before {
		t.Fatal("hover expansion must overlay rather than resize the workspace")
	}
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, X: 500, Y: 300, Viewport: size})
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.verticalTabs.bounds.Width != verticalTabsRail || state.workspacePixelBounds(size) != before {
		t.Fatal("moving away must collapse the sidebar without resizing")
	}
	state.Click(hitVerticalTabsPin)
	if state.verticalTabs.autoHide || state.workspacePixelBounds(size).X != verticalTabsWidth {
		t.Fatal("pinning must keep the sidebar expanded")
	}
}

func TestVerticalTabsLeaveRoomForFilesAndAssistant(t *testing.T) {
	state := newTestState(t, Options{})
	openEmptyTab(state)
	state.setTabOrientation(tabsVertical)
	workspace := state.deck.ActiveWorkspace()
	size := ui.Size{Width: 1600, Height: 900}
	state.ai.open = true
	for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
		if err := state.deck.SetContext(workspace, layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Side: side, Width: layoutcore.DefaultContextWidth}); err != nil {
			t.Fatal(err)
		}
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		files, panes := state.contextRect, state.workspacePixelBounds(size)
		if files.Width <= 0 || files.X < verticalTabsWidth || panes.X < verticalTabsWidth ||
			files.X+files.Width > size.Width-state.aiReserve(workspace, size)+1 {
			t.Fatalf("panels overlap the tabs or assistant: files=%+v panes=%+v", files, panes)
		}
		if side == layoutcore.ContextLeft && files.X+files.Width > panes.X ||
			side == layoutcore.ContextRight && panes.X+panes.Width > files.X {
			t.Fatalf("files overlap terminal panes: files=%+v panes=%+v", files, panes)
		}
	}
}
