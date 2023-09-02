// Package list provides a feature-rich Bubble Tea component for scrolling
// a general purpose list of items. It features optional filtering,
// help, status messages, and a spinner to indicate activity.
package list

// visibleItems
// itemsInView

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/sahilm/fuzzy"
)

// Item is an item that appears in the list.
type Item interface {
	// FilterValue is the value we use when filtering against this item when
	// we're filtering the list.
	FilterValue() string
}

// ItemDelegate encapsulates the general functionality for all list items. The
// benefit to separating this logic from the item itself is that you can change
// the functionality of items without changing the actual items themselves.
//
// Note that if the delegate also implements help.KeyMap delegate-related
// help items will be added to the help view.
type ItemDelegate interface {
	// Render renders the item's view.
	Render(w io.Writer, m Model, index int, item Item)

	// Height is the height of the list item.
	Height() int

	// Spacing is the size of the horizontal gap between list items in cells.
	Spacing() int

	// Update is the update loop for items. All messages in the list's update
	// loop will pass through here except when the user is setting a filter.
	// Use this method to perform item-level updates appropriate to this
	// delegate.
	Update(msg tea.Msg, m *Model) tea.Cmd
}

type filteredItem struct {
	item    Item  // item matched
	matches []int // rune indices of matched items
}

type filteredItems []filteredItem

func (f filteredItems) items() []Item {
	agg := make([]Item, len(f))
	for i, v := range f {
		agg[i] = v.item
	}
	return agg
}

// FilterMatchesMsg contains data about items matched during filtering. The
// message should be routed to Update for processing.
type FilterMatchesMsg []filteredItem

// FilterFunc takes a term and a list of strings to search through
// (defined by Item#FilterValue).
// It should return a sorted list of ranks.
type FilterFunc func(string, []string) []Rank

// Rank defines a rank for a given item.
type Rank struct {
	// The index of the item in the original input.
	Index int
	// Indices of the actual word that were matched against the filter term.
	MatchedIndexes []int
}

// DefaultFilter uses the sahilm/fuzzy to filter through the list.
// This is set by default.
func DefaultFilter(term string, targets []string) []Rank {
	ranks := fuzzy.Find(term, targets)
	sort.Stable(ranks)
	result := make([]Rank, len(ranks))
	for i, r := range ranks {
		result[i] = Rank{
			Index:          r.Index,
			MatchedIndexes: r.MatchedIndexes,
		}
	}
	return result
}

// UnsortedFilter uses the sahilm/fuzzy to filter through the list. It does not
// sort the results.
func UnsortedFilter(term string, targets []string) []Rank {
	ranks := fuzzy.FindNoSort(term, targets)
	result := make([]Rank, len(ranks))
	for i, r := range ranks {
		result[i] = Rank{
			Index:          r.Index,
			MatchedIndexes: r.MatchedIndexes,
		}
	}
	return result
}

type statusMessageTimeoutMsg struct{}

// FilterState describes the current filtering state on the model.
type FilterState int

// Possible filter states.
const (
	Unfiltered    FilterState = iota // no filter set
	Filtering                        // user is actively setting a filter
	FilterApplied                    // a filter is applied and user is not editing filter
)

// String returns a human-readable string of the current filter state.
func (f FilterState) String() string {
	return [...]string{
		"unfiltered",
		"filtering",
		"filter applied",
	}[f]
}

// Model contains the state of this component.
type Model struct {
	showTitle        bool
	showFilter       bool
	showStatusBar    bool
	showHelp         bool
	filteringEnabled bool

	itemNameSingular string
	itemNamePlural   string

	Title             string
	Styles            Styles
	InfiniteScrolling bool

	// Key mappings for navigating the list.
	KeyMap KeyMap

	// Filter is used to filter the list.
	Filter FilterFunc

	disableQuitKeybindings bool

	// Additional key mappings for the short and full help views. This allows
	// you to add additional key mappings to the help menu without
	// re-implementing the help component. Of course, you can also disable the
	// list's help component and implement a new one if you need more
	// flexibility.
	AdditionalShortHelpKeys func() []key.Binding
	AdditionalFullHelpKeys  func() []key.Binding

	spinner     spinner.Model
	showSpinner bool
	width       int
	height      int
	Help        help.Model
	FilterInput textinput.Model
	filterState FilterState

	// How long status messages should stay visible. By default this is
	// 1 second.
	StatusMessageLifetime time.Duration

	statusMessage      string
	statusMessageTimer *time.Timer

	// The master set of items we're working with.
	items []Item

	// The index of the item selected in the AvailableItems()
	// If AvailableItems() is empty, index is set to -1.
	index int
	// The index of item in the AvailableItems() being shown
	// at the top of the list viewport.
	firstItemIndexInView int
	// The index of item in the AvailableItems() being shown
	// at the bottom of the list viewport.
	lastItemIndexInView int

	// Filtered items we're currently displaying. Filtering, toggles and so on
	// will alter this slice so we can show what is relevant. For that reason,
	// this field should be considered ephemeral.
	filteredItems filteredItems

	delegate ItemDelegate
}

// New returns a new model with sensible defaults.
func New(items []Item, delegate ItemDelegate, width, height int) Model {
	styles := DefaultStyles()

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = styles.Spinner

	filterInput := textinput.New()
	filterInput.Prompt = "Filter: "
	filterInput.PromptStyle = styles.FilterPrompt
	filterInput.Cursor.Style = styles.FilterCursor
	filterInput.CharLimit = 64
	filterInput.Focus()

	index := -1
	if len(items) > 0 {
		index = 0
	}

	m := Model{
		showTitle:             true,
		showFilter:            true,
		showStatusBar:         true,
		showHelp:              true,
		itemNameSingular:      "item",
		itemNamePlural:        "items",
		filteringEnabled:      true,
		KeyMap:                DefaultKeyMap(),
		Filter:                DefaultFilter,
		Styles:                styles,
		Title:                 "List",
		FilterInput:           filterInput,
		StatusMessageLifetime: time.Second,

		width:    width,
		height:   height,
		delegate: delegate,
		items:    items,
		index:    index,
		spinner:  sp,
		Help:     help.New(),
	}

	m.updateKeybindings()
	return m
}

// NewModel returns a new model with sensible defaults.
//
// Deprecated: use [New] instead.
var NewModel = New

// SetFilteringEnabled enables or disables filtering. Note that this is different
// from ShowFilter, which merely hides or shows the input view.
func (m *Model) SetFilteringEnabled(v bool) {
	m.filteringEnabled = v
	if !v {
		m.resetFiltering()
	}
	m.updateKeybindings()
}

// FilteringEnabled returns whether or not filtering is enabled.
func (m Model) FilteringEnabled() bool {
	return m.filteringEnabled
}

// SetShowTitle shows or hides the title bar.
func (m *Model) SetShowTitle(v bool) {
	m.showTitle = v
}

// ShowTitle returns whether or not the title bar is set to be rendered.
func (m Model) ShowTitle() bool {
	return m.showTitle
}

// SetShowFilter shows or hides the filter bar. Note that this does not disable
// filtering, it simply hides the built-in filter view. This allows you to
// use the FilterInput to render the filtering UI differently without having to
// re-implement filtering from scratch.
//
// To disable filtering entirely use EnableFiltering.
func (m *Model) SetShowFilter(v bool) {
	m.showFilter = v
}

// ShowFilter returns whether or not the filter is set to be rendered. Note
// that this is separate from FilteringEnabled, so filtering can be hidden yet
// still invoked. This allows you to render filtering differently without
// having to re-implement it from scratch.
func (m Model) ShowFilter() bool {
	return m.showFilter
}

// SetShowStatusBar shows or hides the view that displays metadata about the
// list, such as item counts.
func (m *Model) SetShowStatusBar(v bool) {
	m.showStatusBar = v
}

// ShowStatusBar returns whether or not the status bar is set to be rendered.
func (m Model) ShowStatusBar() bool {
	return m.showStatusBar
}

// SetStatusBarItemName defines a replacement for the item's identifier.
// Defaults to item/items.
func (m *Model) SetStatusBarItemName(singular, plural string) {
	m.itemNameSingular = singular
	m.itemNamePlural = plural
}

// StatusBarItemName returns singular and plural status bar item names.
func (m Model) StatusBarItemName() (string, string) {
	return m.itemNameSingular, m.itemNamePlural
}

// SetShowHelp shows or hides the help view.
func (m *Model) SetShowHelp(v bool) {
	m.showHelp = v
}

// ShowHelp returns whether or not the help is set to be rendered.
func (m Model) ShowHelp() bool {
	return m.showHelp
}

// Items returns the items in the list.
func (m Model) Items() []Item {
	return m.items
}

// SetItems sets the items available in the list. This returns a command.
func (m *Model) SetItems(i []Item) tea.Cmd {
	var cmd tea.Cmd
	m.items = i

	if m.filterState != Unfiltered {
		m.filteredItems = nil
		cmd = filterItems(*m)
	}

	m.updateKeybindings()
	return cmd
}

// Select selects the given index of the list and scrolls to it if needed.
func (m *Model) Select(index int) {
	size := len(m.AvailableItems())

	if size == 0 {
		m.index = -1
		return
	}

	if index < 0 {
		m.index = 0
		return
	}
	if index > (size - 1) {
		m.index = size - 1
		return
	}

	m.index = index
}

// ResetSelected resets the selected item to the first item in the list.
func (m *Model) ResetSelected() {
	m.Select(0)
}

// ResetFilter resets the current filtering state.
func (m *Model) ResetFilter() {
	m.resetFiltering()
}

// SetItem replaces an item at the given index. This returns a command.
func (m *Model) SetItem(index int, item Item) tea.Cmd {
	var cmd tea.Cmd
	m.items[index] = item

	if m.filterState != Unfiltered {
		cmd = filterItems(*m)
	}

	return cmd
}

// MoveItemUp method swaps the current item with the one above it in the list.
func (m *Model) MoveItemUp(index int) {
	if m.filterState == Unfiltered {
		m.items = swapItemsInSlice(m.items, index, index-1)
		m.CursorUp()
	}
}

// MoveItemDown method swaps the current item with the one below it in the list.
func (m *Model) MoveItemDown(index int) {
	if m.filterState == Unfiltered {
		m.items = swapItemsInSlice(m.items, index, index+1)
		m.CursorDown()
	}
}

// InsertItem inserts an item at the given index. If the index is out of the upper bound,
// the item will be appended. This returns a command.
func (m *Model) InsertItem(index int, item Item) tea.Cmd {
	var cmd tea.Cmd
	m.items = insertItemIntoSlice(m.items, item, index)

	if m.filterState != Unfiltered {
		cmd = filterItems(*m)
	}

	m.updateKeybindings()
	return cmd
}

// RemoveItem removes an item at the given index. If the index is out of bounds
// this will be a no-op. O(n) complexity, which probably won't matter in the
// case of a TUI.
func (m *Model) RemoveItem(index int) {
	m.items = removeItemFromSlice(m.items, index)
	if m.filterState != Unfiltered {
		m.filteredItems = removeFilterMatchFromSlice(m.filteredItems, index)
		if len(m.filteredItems) == 0 {
			m.resetFiltering()
		}
	}
}

// SetDelegate sets the item delegate.
func (m *Model) SetDelegate(d ItemDelegate) {
	m.delegate = d
}

// AvailableItems returns the total items available to be shown.
func (m Model) AvailableItems() []Item {
	if m.filterState != Unfiltered {
		return m.filteredItems.items()
	}
	return m.items
}

// SelectedItem returns the current selected item in the list.
func (m Model) SelectedItem() Item {
	i := m.Index()

	items := m.AvailableItems()
	if i < 0 || len(items) == 0 || len(items) <= i {
		return nil
	}

	return items[i]
}

// MatchesForItem returns rune positions matched by the current filter, if any.
// Use this to style runes matched by the active filter.
//
// See DefaultItemView for a usage example.
func (m Model) MatchesForItem(index int) []int {
	if m.filteredItems == nil || index >= len(m.filteredItems) {
		return nil
	}
	return m.filteredItems[index].matches
}

// Index returns the index of the currently selected item as it appears in the
// entire slice of items. If there are no items, returns -1.
func (m Model) Index() int {
	return m.index
}

// CursorUp selects the previous item.
func (m *Model) CursorUp() {
	m.Select(m.index - 1)
}

// CursorDown selects the next item.
func (m *Model) CursorDown() {
	m.Select(m.index + 1)
}

// FilterState returns the current filter state.
func (m Model) FilterState() FilterState {
	return m.filterState
}

// FilterValue returns the current value of the filter.
func (m Model) FilterValue() string {
	return m.FilterInput.Value()
}

// SettingFilter returns whether or not the user is currently editing the
// filter value. It's purely a convenience method for the following:
//
//	m.FilterState() == Filtering
//
// It's included here because it's a common thing to check for when
// implementing this component.
func (m Model) SettingFilter() bool {
	return m.filterState == Filtering
}

// IsFiltered returns whether or not the list is currently filtered.
// It's purely a convenience method for the following:
//
//	m.FilterState() == FilterApplied
func (m Model) IsFiltered() bool {
	return m.filterState == FilterApplied
}

// Width returns the current width setting.
func (m Model) Width() int {
	return m.width
}

// Height returns the current height setting.
func (m Model) Height() int {
	return m.height
}

// SetSpinner allows to set the spinner style.
func (m *Model) SetSpinner(spinner spinner.Spinner) {
	m.spinner.Spinner = spinner
}

// ToggleSpinner toggles the spinner. Note that this also returns a command.
func (m *Model) ToggleSpinner() tea.Cmd {
	if !m.showSpinner {
		return m.StartSpinner()
	}
	m.StopSpinner()
	return nil
}

// StartSpinner starts the spinner. Note that this returns a command.
func (m *Model) StartSpinner() tea.Cmd {
	m.showSpinner = true
	return m.spinner.Tick
}

// StopSpinner stops the spinner.
func (m *Model) StopSpinner() {
	m.showSpinner = false
}

// DisableQuitKeybindings is a helper for disabling the keybindings used for quitting,
// in case you want to handle this elsewhere in your application.
func (m *Model) DisableQuitKeybindings() {
	m.disableQuitKeybindings = true
	m.KeyMap.Quit.SetEnabled(false)
	m.KeyMap.ForceQuit.SetEnabled(false)
}

// NewStatusMessage sets a new status message, which will show for a limited
// amount of time. Note that this also returns a command.
func (m *Model) NewStatusMessage(s string) tea.Cmd {
	m.statusMessage = s
	if m.statusMessageTimer != nil {
		m.statusMessageTimer.Stop()
	}

	m.statusMessageTimer = time.NewTimer(m.StatusMessageLifetime)

	// Wait for timeout
	return func() tea.Msg {
		<-m.statusMessageTimer.C
		return statusMessageTimeoutMsg{}
	}
}

// SetSize sets the width and height of this component.
func (m *Model) SetSize(width, height int) {
	m.setSize(width, height)
}

// SetWidth sets the width of this component.
func (m *Model) SetWidth(v int) {
	m.setSize(v, m.height)
}

// SetHeight sets the height of this component.
func (m *Model) SetHeight(v int) {
	m.setSize(m.width, v)
}

func (m *Model) setSize(width, height int) {
	promptWidth := lipgloss.Width(m.Styles.Title.Render(m.FilterInput.Prompt))

	m.width = width
	m.height = height
	m.Help.Width = width
	m.FilterInput.Width = width - promptWidth - lipgloss.Width(m.spinnerView())
}

func (m *Model) resetFiltering() {
	if m.filterState == Unfiltered {
		return
	}

	m.filterState = Unfiltered
	m.FilterInput.Reset()
	m.filteredItems = nil
	m.updateKeybindings()
}

func (m Model) itemsAsFilterItems() filteredItems {
	fi := make([]filteredItem, len(m.items))
	for i, item := range m.items {
		fi[i] = filteredItem{
			item: item,
		}
	}
	return fi
}

// Set keybindings according to the filter state.
func (m *Model) updateKeybindings() {
	switch m.filterState {
	case Filtering:
		m.KeyMap.MoveUp.SetEnabled(false)
		m.KeyMap.MoveDown.SetEnabled(false)
		m.KeyMap.CursorUp.SetEnabled(false)
		m.KeyMap.CursorDown.SetEnabled(false)
		m.KeyMap.GoToStart.SetEnabled(false)
		m.KeyMap.GoToEnd.SetEnabled(false)
		m.KeyMap.Filter.SetEnabled(false)
		m.KeyMap.ClearFilter.SetEnabled(false)
		m.KeyMap.CancelWhileFiltering.SetEnabled(true)
		m.KeyMap.AcceptWhileFiltering.SetEnabled(m.FilterInput.Value() != "")
		m.KeyMap.Quit.SetEnabled(false)
		m.KeyMap.ShowFullHelp.SetEnabled(false)
		m.KeyMap.CloseFullHelp.SetEnabled(false)

	default:
		hasItems := len(m.items) != 0
		m.KeyMap.MoveUp.SetEnabled(hasItems)
		m.KeyMap.MoveDown.SetEnabled(hasItems)
		m.KeyMap.CursorUp.SetEnabled(hasItems)
		m.KeyMap.CursorDown.SetEnabled(hasItems)

		m.KeyMap.GoToStart.SetEnabled(hasItems)
		m.KeyMap.GoToEnd.SetEnabled(hasItems)

		m.KeyMap.Filter.SetEnabled(m.filteringEnabled && hasItems)
		m.KeyMap.ClearFilter.SetEnabled(m.filterState == FilterApplied)
		m.KeyMap.CancelWhileFiltering.SetEnabled(false)
		m.KeyMap.AcceptWhileFiltering.SetEnabled(false)
		m.KeyMap.Quit.SetEnabled(!m.disableQuitKeybindings)

		if m.Help.ShowAll {
			m.KeyMap.ShowFullHelp.SetEnabled(true)
			m.KeyMap.CloseFullHelp.SetEnabled(true)
		} else {
			minHelp := countEnabledBindings(m.FullHelp()) > 1
			m.KeyMap.ShowFullHelp.SetEnabled(minHelp)
			m.KeyMap.CloseFullHelp.SetEnabled(minHelp)
		}
	}
}

// Update viewport according to the amount of items for the current state.
func (m *Model) updateViewportBounds() {
	index := m.Index()
	if index < 0 {
		m.firstItemIndexInView, m.lastItemIndexInView = 0, 0
		return
	}

	availHeight := m.height

	if m.showTitle || (m.showFilter && m.filteringEnabled) {
		availHeight -= lipgloss.Height(m.titleView())
	}
	if m.showStatusBar {
		availHeight -= lipgloss.Height(m.statusView())
	}
	if m.showHelp {
		availHeight -= lipgloss.Height(m.helpView())
	}

	itemHeight := m.delegate.Height() + m.delegate.Spacing()
	availSpace := max(
		1,
		availHeight/itemHeight,
	)

	availItems := m.AvailableItems()
	requiredSpace := len(availItems)

	currentFirst := m.firstItemIndexInView
	currentLast := min(requiredSpace, currentFirst+availSpace) - 1

	// If selected item already in viewport, do nothing.
	if (currentFirst <= index) && (index <= currentLast) {
		m.lastItemIndexInView = currentLast
		return
	}

	// If selected item is below the bottom of view port
	// scroll the view port till the bottom reaches selected item.
	if currentLast < index {
		m.firstItemIndexInView = max(0, index-availSpace+1)
		m.lastItemIndexInView = index
		return
	}

	// If selected item is above the top of view port
	// scroll the view port till the top reaches selected item.
	if currentFirst > index {
		m.firstItemIndexInView = index
		m.lastItemIndexInView = index + min(requiredSpace, availSpace) - 1
		return
	}
}

func (m *Model) hideStatusMessage() {
	m.statusMessage = ""
	if m.statusMessageTimer != nil {
		m.statusMessageTimer.Stop()
	}
}

// Update is the Bubble Tea update loop.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, m.KeyMap.ForceQuit) {
			return m, tea.Quit
		}

	case FilterMatchesMsg:
		m.filteredItems = filteredItems(msg)
		return m, nil

	case spinner.TickMsg:
		newSpinnerModel, cmd := m.spinner.Update(msg)
		m.spinner = newSpinnerModel
		if m.showSpinner {
			cmds = append(cmds, cmd)
		}

	case statusMessageTimeoutMsg:
		m.hideStatusMessage()
	}

	if m.filterState == Filtering {
		cmds = append(cmds, m.handleFiltering(msg))
	} else {
		cmds = append(cmds, m.handleBrowsing(msg))
		cmds = append(cmds, m.handleMoving(msg))
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleMoving(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.MoveUp):
			m.MoveItemUp(m.Index())

		case key.Matches(msg, m.KeyMap.MoveDown):
			m.MoveItemDown(m.Index())
		}
	}

	cmd := m.delegate.Update(msg, m)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// Updates for when a user is browsing the list.
func (m *Model) handleBrowsing(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		// Note: we match clear filter before quit because, by default, they're
		// both mapped to escape.
		case key.Matches(msg, m.KeyMap.ClearFilter):
			m.resetFiltering()

		case key.Matches(msg, m.KeyMap.Quit):
			return tea.Quit

		case key.Matches(msg, m.KeyMap.CursorUp):
			m.CursorUp()

		case key.Matches(msg, m.KeyMap.CursorDown):
			m.CursorDown()

		case key.Matches(msg, m.KeyMap.GoToStart):
			m.ResetSelected()

		case key.Matches(msg, m.KeyMap.GoToEnd):
			m.Select(len(m.items))

		case key.Matches(msg, m.KeyMap.Filter):
			m.hideStatusMessage()
			if m.FilterInput.Value() == "" {
				// Populate filter with all items only if the filter is empty.
				m.filteredItems = m.itemsAsFilterItems()
			}
			m.ResetSelected()
			m.filterState = Filtering
			m.FilterInput.CursorEnd()
			m.FilterInput.Focus()
			m.updateKeybindings()
			return textinput.Blink

		case key.Matches(msg, m.KeyMap.ShowFullHelp):
			fallthrough
		case key.Matches(msg, m.KeyMap.CloseFullHelp):
			m.Help.ShowAll = !m.Help.ShowAll
		}
	}

	cmd := m.delegate.Update(msg, m)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// Updates for when a user is in the filter editing interface.
func (m *Model) handleFiltering(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	// Handle keys
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.KeyMap.CancelWhileFiltering):
			m.resetFiltering()
			m.KeyMap.Filter.SetEnabled(true)
			m.KeyMap.ClearFilter.SetEnabled(false)

		case key.Matches(msg, m.KeyMap.AcceptWhileFiltering):
			m.hideStatusMessage()

			if len(m.items) == 0 {
				break
			}

			h := m.AvailableItems()

			// If we've filtered down to nothing, clear the filter
			if len(h) == 0 {
				m.resetFiltering()
				break
			}

			m.FilterInput.Blur()
			m.filterState = FilterApplied
			m.updateKeybindings()

			if m.FilterInput.Value() == "" {
				m.resetFiltering()
			}
		}
	}

	// Update the filter text input component
	newFilterInputModel, inputCmd := m.FilterInput.Update(msg)
	filterChanged := m.FilterInput.Value() != newFilterInputModel.Value()
	m.FilterInput = newFilterInputModel
	cmds = append(cmds, inputCmd)

	// If the filtering input has changed, request updated filtering
	if filterChanged {
		cmds = append(cmds, filterItems(*m))
		m.KeyMap.AcceptWhileFiltering.SetEnabled(m.FilterInput.Value() != "")
	}

	return tea.Batch(cmds...)
}

// ShortHelp returns bindings to show in the abbreviated help view. It's part
// of the help.KeyMap interface.
func (m Model) ShortHelp() []key.Binding {
	kb := []key.Binding{
		m.KeyMap.CursorUp,
		m.KeyMap.CursorDown,
	}

	filtering := m.filterState == Filtering

	// If the delegate implements the help.KeyMap interface add the short help
	// items to the short help after the cursor movement keys.
	if !filtering {
		if b, ok := m.delegate.(help.KeyMap); ok {
			kb = append(kb, b.ShortHelp()...)
		}
	}

	kb = append(kb,
		m.KeyMap.Filter,
		m.KeyMap.ClearFilter,
		m.KeyMap.AcceptWhileFiltering,
		m.KeyMap.CancelWhileFiltering,
	)

	if !filtering && m.AdditionalShortHelpKeys != nil {
		kb = append(kb, m.AdditionalShortHelpKeys()...)
	}

	return append(kb,
		m.KeyMap.Quit,
		m.KeyMap.ShowFullHelp,
	)
}

// FullHelp returns bindings to show the full help view. It's part of the
// help.KeyMap interface.
func (m Model) FullHelp() [][]key.Binding {
	kb := [][]key.Binding{{
		m.KeyMap.CursorUp,
		m.KeyMap.CursorDown,
		m.KeyMap.MoveUp,
		m.KeyMap.MoveDown,
		m.KeyMap.GoToStart,
		m.KeyMap.GoToEnd,
	}}

	filtering := m.filterState == Filtering

	// If the delegate implements the help.KeyMap interface add full help
	// keybindings to a special section of the full help.
	if !filtering {
		if b, ok := m.delegate.(help.KeyMap); ok {
			kb = append(kb, b.FullHelp()...)
		}
	}

	listLevelBindings := []key.Binding{
		m.KeyMap.Filter,
		m.KeyMap.ClearFilter,
		m.KeyMap.AcceptWhileFiltering,
		m.KeyMap.CancelWhileFiltering,
	}

	if !filtering && m.AdditionalFullHelpKeys != nil {
		listLevelBindings = append(
			listLevelBindings,
			m.AdditionalFullHelpKeys()...)
	}

	return append(kb,
		listLevelBindings,
		[]key.Binding{
			m.KeyMap.Quit,
			m.KeyMap.CloseFullHelp,
		})
}

// View renders the component.
func (m Model) View() string {
	var (
		sections    []string
		availHeight = m.height
	)

	if m.showTitle || (m.showFilter && m.filteringEnabled) {
		v := m.titleView()
		sections = append(sections, v)
		availHeight -= lipgloss.Height(v)
	}

	if m.showStatusBar {
		v := m.statusView()
		sections = append(sections, v)
		availHeight -= lipgloss.Height(v)
	}

	var help string
	if m.showHelp {
		help = m.helpView()
		availHeight -= lipgloss.Height(help)
	}

	content := lipgloss.NewStyle().Height(availHeight).Render(m.populatedView())
	sections = append(sections, content)

	if m.showHelp {
		sections = append(sections, help)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) titleView() string {
	var (
		view          string
		titleBarStyle = m.Styles.TitleBar.Copy()

		// We need to account for the size of the spinner, even if we don't
		// render it, to reserve some space for it should we turn it on later.
		spinnerView    = m.spinnerView()
		spinnerWidth   = lipgloss.Width(spinnerView)
		spinnerLeftGap = " "
		spinnerOnLeft  = titleBarStyle.GetPaddingLeft() >= spinnerWidth+lipgloss.Width(
			spinnerLeftGap,
		) &&
			m.showSpinner
	)

	// If the filter's showing, draw that. Otherwise draw the title.
	if m.showFilter && m.filterState == Filtering {
		view += m.FilterInput.View()
	} else if m.showTitle {
		if m.showSpinner && spinnerOnLeft {
			view += spinnerView + spinnerLeftGap
			titleBarGap := titleBarStyle.GetPaddingLeft()
			titleBarStyle = titleBarStyle.PaddingLeft(titleBarGap - spinnerWidth - lipgloss.Width(spinnerLeftGap))
		}

		view += m.Styles.Title.Render(m.Title)

		// Status message
		if m.filterState != Filtering {
			view += "  " + m.statusMessage
			view = truncate.StringWithTail(view, uint(m.width-spinnerWidth), ellipsis)
		}
	}

	// Spinner
	if m.showSpinner && !spinnerOnLeft {
		// Place spinner on the right
		availSpace := m.width - lipgloss.Width(m.Styles.TitleBar.Render(view))
		if availSpace > spinnerWidth {
			view += strings.Repeat(" ", availSpace-spinnerWidth)
			view += spinnerView
		}
	}

	if len(view) > 0 {
		return titleBarStyle.Render(view)
	}
	return view
}

func (m Model) statusView() string {
	var status string

	totalItems := len(m.items)
	availableItems := len(m.AvailableItems())

	var itemName string
	if availableItems != 1 {
		itemName = m.itemNamePlural
	} else {
		itemName = m.itemNameSingular
	}

	itemsDisplay := fmt.Sprintf("%d %s", availableItems, itemName)

	if m.filterState == Filtering {
		// Filter results
		if availableItems == 0 {
			status = m.Styles.StatusEmpty.Render("Nothing matched")
		} else {
			status = itemsDisplay
		}
	} else if len(m.items) == 0 {
		// Not filtering: no items.
		status = m.Styles.StatusEmpty.Render("No " + m.itemNamePlural)
	} else {
		// Normal
		filtered := m.FilterState() == FilterApplied

		if filtered {
			f := strings.TrimSpace(m.FilterInput.Value())
			f = truncate.StringWithTail(f, 10, "…")
			status += fmt.Sprintf("“%s” ", f)
		}

		status += itemsDisplay
	}

	numFiltered := totalItems - availableItems
	if numFiltered > 0 {
		status += m.Styles.DividerDot.String()
		status += m.Styles.StatusBarFilterCount.Render(
			fmt.Sprintf("%d filtered", numFiltered),
		)
	}
	// status += " i:" + fmt.Sprint(
	// 	m.index,
	// ) + " f:" + fmt.Sprint(
	// 	m.firstItemIndexInView,
	// ) + " l:" + fmt.Sprint(
	// 	m.lastItemIndexInView,
	// ) + " s:" + fmt.Sprint(
	// 	len(m.AvailableItems()),
	// )

	return m.Styles.StatusBar.Render(status)
}

func (m Model) populatedView() string {
	m.updateViewportBounds()
	items := m.AvailableItems()

	var b strings.Builder

	// Empty states
	if len(items) == 0 {
		if m.filterState == Filtering {
			return ""
		}
		return m.Styles.NoItems.Render("No " + m.itemNamePlural + ".")
	}

	if len(items) > 0 {
		start := m.firstItemIndexInView
		docs := items[m.firstItemIndexInView : m.lastItemIndexInView+1]

		for i, item := range docs {
			m.delegate.Render(&b, m, i+start, item)
			if i != len(docs)-1 {
				fmt.Fprint(
					&b,
					strings.Repeat("\n", m.delegate.Spacing()+1),
				)
			}
		}
	}

	return b.String()
}

func (m Model) helpView() string {
	return m.Styles.HelpStyle.Render(m.Help.View(m))
}

func (m Model) spinnerView() string {
	return m.spinner.View()
}

func filterItems(m Model) tea.Cmd {
	return func() tea.Msg {
		if m.FilterInput.Value() == "" || m.filterState == Unfiltered {
			return FilterMatchesMsg(m.itemsAsFilterItems()) // return nothing
		}

		items := m.items
		targets := make([]string, len(items))

		for i, t := range items {
			targets[i] = t.FilterValue()
		}

		filterMatches := []filteredItem{}
		for _, r := range m.Filter(m.FilterInput.Value(), targets) {
			filterMatches = append(filterMatches, filteredItem{
				item:    items[r.Index],
				matches: r.MatchedIndexes,
			})
		}

		return FilterMatchesMsg(filterMatches)
	}
}

func swapItemsInSlice(items []Item, firstIndex, secondIndex int) []Item {
	if items == nil {
		return items
	}
	maxIndex := len(items) - 1

	firstIndex = setInBounds(firstIndex, 0, maxIndex)
	secondIndex = setInBounds(secondIndex, 0, maxIndex)

	items[firstIndex], items[secondIndex] = items[secondIndex], items[firstIndex]
	return items
}

func setInBounds(x, low, high int) int {
	return min(high, max(x, low))
}

func insertItemIntoSlice(items []Item, item Item, index int) []Item {
	if items == nil {
		return []Item{item}
	}
	if index >= len(items) {
		return append(items, item)
	}

	index = max(0, index)

	items = append(items, nil)
	copy(items[index+1:], items[index:])
	items[index] = item
	return items
}

// Remove an item from a slice of items at the given index. This runs in O(n).
func removeItemFromSlice(i []Item, index int) []Item {
	if index >= len(i) {
		return i // noop
	}
	copy(i[index:], i[index+1:])
	i[len(i)-1] = nil
	return i[:len(i)-1]
}

func removeFilterMatchFromSlice(i []filteredItem, index int) []filteredItem {
	if index >= len(i) {
		return i // noop
	}
	copy(i[index:], i[index+1:])
	i[len(i)-1] = filteredItem{}
	return i[:len(i)-1]
}

func countEnabledBindings(groups [][]key.Binding) (agg int) {
	for _, group := range groups {
		for _, kb := range group {
			if kb.Enabled() {
				agg++
			}
		}
	}
	return agg
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
