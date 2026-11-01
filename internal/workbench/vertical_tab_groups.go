package workbench

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

const verticalTabIndent = float32(14)

// Grouping is only a view of the deck. It never reorders, merges or reconnects
// sessions; disabling it restores the exact flat tab list.
type verticalTabKey struct {
	protocol           hostProtocol
	address, user      string
	port               uint16
	jump, proxy, shell string
	local              bool
	workspace          layoutcore.WorkspaceID // nonzero: an empty or mixed-host tab
}

type verticalTabGroup struct {
	key         verticalTabKey
	title       string
	label       string
	countText   string
	labelCount  int
	labelFormat string
	members     [layoutcore.MaxWorkspaces]int
	count       int
	active      bool
	badge       flex.Box
}

func (group *verticalTabGroup) id(tabs []layoutcore.WorkspaceInfo) ui.ID {
	return hitVerticalTabGroups.At(int(tabs[group.members[0]].ID))
}

type verticalTabRow struct {
	group    int
	tab      int // -1 is a group header
	indented bool
}

type verticalTabGrouping struct {
	groups    []verticalTabGroup
	rows      []verticalTabRow
	order     []layoutcore.WorkspaceInfo // all tabs, in grouped visual order
	layout    []layoutcore.WorkspaceInfo // visible rows for the scroll strip
	options   []components.TabOption     // visible keyboard targets, including headers
	collapsed map[verticalTabKey]bool
	revealed  layoutcore.WorkspaceID
	compact   bool
}

func (state *State) verticalTabsGrouped() bool {
	return state.tabOrientation == tabsVertical && state.verticalTabs.grouping
}

func (state *State) setVerticalTabsGrouping(enabled bool) bool {
	if state.verticalTabs.grouping == enabled {
		return false
	}
	state.verticalTabs.grouping = enabled
	state.verticalTabs.grouped.revealed = 0
	state.tabStrip.revealed = 0
	return true
}

func (state *State) verticalSessionKey(session *Session) verticalTabKey {
	host := session.host
	if session.isLocal() {
		return verticalTabKey{local: true, shell: host.localShell}
	}
	address := strings.TrimSpace(host.Address)
	user := strings.TrimSpace(host.User)
	if host.isSSH() {
		user = state.login(host)
	}
	if host.isSSH() && session.endpoint.Host != "" {
		address, user = session.endpoint.Host, session.endpoint.User
	}
	if host.Protocol != protocolSerial {
		address = strings.Trim(address, "[]")
		if ip, err := netip.ParseAddr(address); err == nil {
			address = ip.String()
		} else {
			address = strings.TrimSuffix(strings.ToLower(address), ".")
		}
	}
	port := host.Port
	if host.isSSH() && session.endpoint.Port != 0 {
		port = session.endpoint.Port
	}
	if port == 0 && host.Protocol != protocolSerial {
		port = host.defaultPort()
	}
	return verticalTabKey{
		protocol: host.Protocol, address: address, port: port, user: user,
		jump: session.jumpPath, proxy: session.proxyRoute,
	}
}

func (state *State) verticalWorkspaceKey(tab layoutcore.WorkspaceInfo) (verticalTabKey, string) {
	var slots [layoutcore.MaxPanes]layoutcore.Pane
	var key verticalTabKey
	var title string
	found := false
	for _, pane := range state.deck.Panes(tab.ID, slots[:0]) {
		if pane.SessionID == 0 {
			continue
		}
		session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
		if !ok {
			return verticalTabKey{workspace: tab.ID}, tab.Title
		}
		candidate := state.verticalSessionKey(session)
		if candidate.address == "" && !candidate.local || found && candidate != key {
			return verticalTabKey{workspace: tab.ID}, tab.Title
		}
		if !found {
			key, title, found = candidate, session.host.Label, true
			if title == "" {
				title = state.sessionAddress(session.host)
			}
		}
	}
	if !found {
		return verticalTabKey{workspace: tab.ID}, tab.Title
	}
	return key, title
}

func (state *State) rebuildVerticalTabGroups(compact bool) {
	view := &state.verticalTabs.grouped
	view.compact = compact
	view.groups = view.groups[:0]
	active := state.deck.ActiveWorkspace()
	for index, tab := range state.workspaceTabs {
		key, title := state.verticalWorkspaceKey(tab)
		groupIndex := -1
		for i := range view.groups {
			if view.groups[i].key == key {
				groupIndex = i
				break
			}
		}
		if groupIndex < 0 {
			groupIndex = len(view.groups)
			if groupIndex < cap(view.groups) {
				view.groups = view.groups[:groupIndex+1]
			} else {
				view.groups = append(view.groups, verticalTabGroup{})
			}
			group := &view.groups[groupIndex]
			if group.key != key || group.title != title {
				group.label = ""
			}
			group.key, group.title, group.count, group.active, group.badge = key, title, 0, false, flex.Box{}
		}
		group := &view.groups[groupIndex]
		group.members[group.count] = index
		group.count++
		group.active = group.active || tab.ID == active && state.terminalPage()
	}
	for key := range view.collapsed {
		found := false
		for _, group := range view.groups {
			found = found || group.key == key
		}
		if !found {
			delete(view.collapsed, key)
		}
	}
	// A shortcut or newly created connection must reveal its child. A manual
	// collapse of the current group remains closed until another tab is selected.
	if state.terminalPage() && active != view.revealed {
		for _, group := range view.groups {
			if group.active {
				delete(view.collapsed, group.key)
			}
		}
	}
	view.revealed = active
	previousActiveRow := -1
	for index, row := range view.layout {
		if row.ID == active {
			previousActiveRow = index
		}
	}
	view.order, view.rows, view.layout, view.options = view.order[:0], view.rows[:0], view.layout[:0], view.options[:0]
	for index := range view.groups {
		group := &view.groups[index]
		if group.label == "" || group.labelCount != group.count || group.labelFormat != state.messages.VerticalTabGroupCountFmt {
			group.label = fmt.Sprintf(state.messages.VerticalTabGroupCountFmt, group.title, group.count)
			group.countText = strconv.Itoa(group.count)
			group.labelCount, group.labelFormat = group.count, state.messages.VerticalTabGroupCountFmt
		}
		if group.count > 1 && !compact {
			id := group.id(state.workspaceTabs)
			view.rows = append(view.rows, verticalTabRow{group: index, tab: -1})
			view.options = append(view.options, components.TabOption{ID: id, Label: group.label})
			row := layoutcore.WorkspaceInfo{}
			if group.active && view.collapsed[group.key] {
				row.ID = active
			}
			view.layout = append(view.layout, row)
		}
		for _, tabIndex := range group.members[:group.count] {
			tab := state.workspaceTabs[tabIndex]
			view.order = append(view.order, tab)
			if group.count > 1 && view.collapsed[group.key] && !compact {
				continue
			}
			view.rows = append(view.rows, verticalTabRow{group: index, tab: tabIndex, indented: group.count > 1 && !compact})
			view.layout = append(view.layout, tab)
			view.options = append(view.options, components.TabOption{ID: hitTabList.At(int(tab.ID)), Label: state.tabLabel(tab)})
		}
	}
	if previousActiveRow >= 0 {
		start := float32(previousActiveRow) * state.tabStrip.pitch
		if start >= state.tabStrip.offset && start+verticalTabHeight <= state.tabStrip.offset+state.tabStrip.area.Height {
			for index, row := range view.layout {
				if row.ID == active && index != previousActiveRow {
					state.tabStrip.revealed = 0
				}
			}
		}
	}
}

func (state *State) declareGroupedVerticalTabs(f *flex.Frame, width float32, compact bool) {
	view := &state.verticalTabs
	for _, row := range view.grouped.rows {
		if row.tab < 0 {
			state.declareVerticalTabGroup(f, row.group, width)
			continue
		}
		indent := float32(0)
		if row.indented {
			indent = verticalTabIndent
		}
		f.Row().Size(width, verticalTabHeight).Shrink(0).MarginSides(0, 0, barGap, 0).Children(func() {
			if indent > 0 {
				f.Slot().Width(indent).Shrink(0)
			}
			view.tabs = append(view.tabs, state.declareWorkspaceTab(f, state.workspaceTabs[row.tab], max(width-indent, 0), verticalTabHeight, 0, true, compact))
		})
	}
}

func (state *State) declareVerticalTabGroup(f *flex.Frame, index int, width float32) {
	view, pal := &state.verticalTabs.grouped, state.pal
	group := &view.groups[index]
	id := group.id(state.workspaceTabs)
	expanded := !view.collapsed[group.key]
	fill := pal.shell
	if group.active {
		fill = pal.surfaceHot
	} else if f.Canvas().Hovered() == id {
		fill = pal.surfaceSoft
	}
	icon := uint8(iconChevronRight)
	if expanded {
		icon = iconChevronDown
	}
	f.Row().Size(width, verticalTabHeight).Shrink(0).MarginSides(0, 0, barGap, 0).PadX(4).Gap(4).Items(flex.Center).
		Radius(radius.item).Background(fill).
		HitWith(id, group.label, ui.HitOptions{Role: ui.RoleButton, Expanded: expanded, Expandable: true, Selected: group.active, Hint: group.label}).Children(func() {
		f.Icon(lucideIcons[icon]).Size(12, 12).Shrink(0).Color(pal.muted)
		group.badge = f.Slot().Size(18, 18).Shrink(0)
		f.Text(group.title).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.text).Ellipsis().Grow(1).Basis(0)
		f.Text(group.countText).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
	})
}

func (state *State) paintVerticalTabGroupIcons(canvas *ui.Canvas) {
	for _, group := range state.verticalTabs.grouped.groups {
		if group.count < 2 || group.badge == (flex.Box{}) {
			continue
		}
		tab := state.workspaceTabs[group.members[0]]
		if pane, ok := state.deck.Pane(tab.ID, tab.ActivePane); ok {
			if session, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok {
				state.paintHostCatalogIcon(canvas, session.host, group.badge.Rect(), state.pal.shell)
			}
		}
	}
}

func (state *State) clickVerticalTabGroup(id ui.ID) bool {
	if !state.verticalTabsGrouped() || id.Scope() != hitVerticalTabGroups {
		return false
	}
	for _, group := range state.verticalTabs.grouped.groups {
		if group.count > 1 && group.id(state.workspaceTabs) == id {
			state.setVerticalTabGroupCollapsed(group.key, !state.verticalTabs.grouped.collapsed[group.key])
			return true
		}
	}
	return true
}

func (state *State) setVerticalTabGroupCollapsed(key verticalTabKey, collapsed bool) {
	view := &state.verticalTabs.grouped
	if !collapsed {
		delete(view.collapsed, key)
		return
	}
	if view.collapsed == nil {
		view.collapsed = make(map[verticalTabKey]bool)
	}
	view.collapsed[key] = true
}

func (state *State) verticalTabGroupKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if !state.verticalTabsGrouped() || event.Target.Scope() != hitTabList && event.Target.Scope() != hitVerticalTabGroups {
		return ui.EventResult{}, false
	}
	view := &state.verticalTabs.grouped
	for _, row := range view.rows {
		group := &view.groups[row.group]
		if row.tab < 0 && group.id(state.workspaceTabs) == event.Target {
			switch event.Key {
			case ui.KeyLeft, ui.KeyRight:
				collapse := event.Key == ui.KeyLeft
				if !collapse && !view.collapsed[group.key] {
					tab := state.workspaceTabs[group.members[0]]
					return state.focusVerticalTabRow(hitTabList.At(int(tab.ID))), true
				}
				state.setVerticalTabGroupCollapsed(group.key, collapse)
				return ui.EventResult{Focus: event.Target, FocusVisible: true, Handled: true, Repaint: true}, true
			}
		} else if row.tab >= 0 && hitTabList.At(int(state.workspaceTabs[row.tab].ID)) == event.Target && row.indented && event.Key == ui.KeyLeft {
			return state.focusVerticalTabRow(group.id(state.workspaceTabs)), true
		}
	}
	next, handled := components.TabsKeyDown(event.Target, view.options, components.TabsVertical, components.DirectionLTR, event.Key)
	if handled {
		return state.focusVerticalTabRow(next), true
	}
	return ui.EventResult{}, true
}

func (state *State) focusVerticalTabRow(id ui.ID) ui.EventResult {
	for index, option := range state.verticalTabs.grouped.options {
		if option.ID == id {
			start := float32(index) * state.tabStrip.pitch
			state.tabStrip.offset = min(state.tabStrip.offset, start)
			state.tabStrip.offset = max(state.tabStrip.offset, start+verticalTabHeight-state.tabStrip.area.Height)
			state.tabStrip.offset = min(max(state.tabStrip.offset, 0), state.tabStrip.max)
			break
		}
	}
	return ui.EventResult{Focus: id, FocusVisible: true, Handled: true, Repaint: true}
}

// Grouped rows and deck positions differ. Resolve a drop to the actual target
// workspace rather than treating a header or hidden child as a tab index.
func (state *State) groupedVerticalTabDrop(at ui.Point, source layoutcore.WorkspaceID) tabDrop {
	view := &state.verticalTabs.grouped
	if len(view.rows) == 0 {
		return tabDrop{}
	}
	strip := &state.tabStrip
	position := (at.Y - strip.origin + strip.offset) / strip.pitch
	rowIndex := min(max(int(position), 0), len(view.rows)-1)
	row := view.rows[rowIndex]
	group := view.groups[row.group]
	tabIndex := row.tab
	if tabIndex < 0 {
		tabIndex = group.members[0]
	}
	after := position-float32(rowIndex) > 0.5
	if row.tab < 0 && after && view.collapsed[group.key] {
		tabIndex = group.members[group.count-1]
	}
	boundary := tabIndex
	if after && (row.tab >= 0 || view.collapsed[group.key]) {
		boundary++
	}
	if source != 0 {
		sourceIndex, ok := state.workspaceTab(source)
		if !ok || boundary == sourceIndex || boundary == sourceIndex+1 {
			return tabDrop{}
		}
		// A connection's host determines its group. Reordering a child into a
		// different server's group would snap straight back, so reject that drop.
		key, _ := state.verticalWorkspaceKey(state.workspaceTabs[sourceIndex])
		if key != group.key {
			if group.count > 1 {
				return tabDrop{}
			}
			for _, sourceGroup := range view.groups {
				if sourceGroup.key == key && sourceGroup.count > 1 {
					return tabDrop{}
				}
			}
		}
	}
	y := strip.origin - strip.offset + float32(rowIndex)*strip.pitch - barGap/2
	if after {
		y += strip.pitch
	}
	return tabDrop{kind: dropReorder, boundary: boundary, zone: state.tabInsertion(y)}
}

func (state *State) visualWorkspaceTabs() []layoutcore.WorkspaceInfo {
	if state.verticalTabsGrouped() {
		state.rebuildVerticalTabGroups(state.verticalTabs.grouped.compact)
		return state.verticalTabs.grouped.order
	}
	return state.workspaceTabs
}

// Reorder only the member slots of this server. Moving its first child past
// another server in the raw deck would otherwise unexpectedly move the group.
func (state *State) reorderVerticalTabGroup(source layoutcore.WorkspaceID, boundary int) bool {
	for _, group := range state.verticalTabs.grouped.groups {
		from := -1
		to := 0
		var members [layoutcore.MaxWorkspaces]layoutcore.WorkspaceID
		for index, slot := range group.members[:group.count] {
			members[index] = state.workspaceTabs[slot].ID
			if members[index] == source {
				from = index
			}
			if slot < boundary {
				to++
			}
		}
		if from < 0 || group.count < 2 {
			continue
		}
		if to > from {
			to--
		}
		if to < from {
			copy(members[to+1:from+1], members[to:from])
		} else {
			copy(members[from:to], members[from+1:to+1])
		}
		members[to] = source
		var order [layoutcore.MaxWorkspaces]layoutcore.WorkspaceID
		for index, tab := range state.workspaceTabs {
			order[index] = tab.ID
		}
		for index, slot := range group.members[:group.count] {
			order[slot] = members[index]
		}
		for index, id := range order[:len(state.workspaceTabs)] {
			_ = state.deck.MoveWorkspace(id, index)
		}
		return true
	}
	return false
}

func (state *State) workspaceTabsAfter(workspace layoutcore.WorkspaceID) []layoutcore.WorkspaceInfo {
	tabs := state.visualWorkspaceTabs()
	for index, tab := range tabs {
		if tab.ID == workspace {
			return tabs[index+1:]
		}
	}
	return nil
}
