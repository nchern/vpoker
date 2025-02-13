package poker

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"

	"github.com/google/uuid"
)

// Table represents a poker table
type Table struct {
	// ID of this table
	ID uuid.UUID `json:"id"`

	// Players represent players in this table
	Players map[uuid.UUID]*Player `json:"players"`

	// Deck represents a deck of cards on the table
	Deck CardList `json:"-"`

	// Chips represnets collection of all chips on the table
	Chips []*Chip `json:"-"`

	// Items on the table
	Items TableItemList `json:"items"`

	lock sync.RWMutex
}

// NewTable creates a new table instance
func NewTable(id uuid.UUID, chipsN int) *Table {
	r := &Table{
		ID:      id,
		Players: map[uuid.UUID]*Player{},
	}
	for _, suit := range []Suit{Spades, Hearts, Diamonds, Clubs} {
		for _, rank := range Ranks {
			r.Deck = append(r.Deck, &Card{Rank: rank, Suit: suit, Side: Cover})
		}
	}
	for _, c := range chipsSet {
		for i := 0; i < chipsN; i++ {
			r.Chips = append(r.Chips, &Chip{Val: c.Val, Color: c.Color})
		}
	}
	return r
}

func shuffle(items []*TableItem) {
	// O(n)
	for i := len(items) - 1; i > 0; i-- {
		j := rand.Intn(i)
		items[i], items[j] = items[j], items[i]
	}
}

// StartGame rearranges all the objects on the table to the initial state
func (t *Table) StartGame() *Table {
	id := 0
	for _, c := range t.Deck {
		t.Items = append(t.Items, NewTableItem(id, 0, 0).AsCard(c))
		id++
	}
	t.Shuffle()
	x := 10
	y := 20
	for i, c := range t.Chips {
		if i > 0 && t.Chips[i-1].Color != c.Color {
			x = 10
			y += 100
		}
		t.Items = append(t.Items, NewTableItem(id, x, y).AsChip(c))
		x++
		id++
	}
	t.Items = append(t.Items, NewTableItem(id, 595, 315).AsDealer())
	return t
}

// Shuffle shuffles cards on the table
func (t *Table) Shuffle() *Table {
	cards := t.Items[0:52]
	shuffle(cards)
	x := 150
	y := 20
	for i, it := range cards {
		it.X = x
		it.Y = y
		it.OwnerID = ""
		it.PrevOwnerID = ""
		it.Side = Cover
		it.ZIndex = i + 10
		x++
	}
	return t
}

func (t *Table) generateChipsForPlayer(idx int) {
	// add chips
	slots := [][]int{
		{140, 545},
		{890, 10},
		{890, 545},
	}
	counts := map[Color]int{
		Gray:  10,
		Red:   8,
		Blue:  5,
		Green: 2,
		Black: 1,
	}
	slot := slots[idx]
	x, y := slot[0], slot[1]
	for _, ci := range chipsSet {
		if ci.Color == Green {
			x = slot[0]
			y = slot[1] + chipWidth
		}
		for i := 0; i < counts[ci.Color]; i++ {
			item := NewTableItem(len(t.Items), x, y).AsChip(&ci)
			t.Items = append(t.Items, item)
			x += 2
		}
		x += chipWidth
	}
}

// Join joins a user
func (t *Table) Join(u *User) []*TableItem {
	index := len(t.Players) % len(playerColors)
	p := newPlayer(u, playerColors[index])
	p.Index = index
	p.Skin = fmt.Sprintf("player_%d", index)

	t.Players[u.ID] = p
	startIdx := len(t.Items)
	t.Items = append(t.Items, NewTableItem(len(t.Items), 0, 0).AsPlayer(p))

	t.generateChipsForPlayer(index)
	return t.Items[startIdx:]
}

// OtherPlayers returns all players but a given
func (t *Table) OtherPlayers(cur *User) PlayerList {
	var others PlayerList
	for _, p := range t.Players {
		if p.ID == cur.ID {
			continue
		}
		others = append(others, p)
	}
	return others
}

// DeepCopy creates a deep copy of this table via serialisation
func (t *Table) DeepCopy() (*Table, error) {
	var dest *Table
	b, err := json.Marshal(&t)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &dest); err != nil {
		return nil, err
	}
	return dest, nil
}

// ReadLock performs thread-safe read of this object
func (t *Table) ReadLock(fn func(*Table) error) error {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return fn(t)
}

// Update performs thread-safe update of this object
func (t *Table) Update(fn func(*Table) error) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	return fn(t)
}

// NotifyOthers notifies all other players at the table except a given one
func (t *Table) NotifyOthers(cur *User, p *Push) {
	t.lock.RLock()
	others := t.OtherPlayers(cur)
	t.lock.RUnlock()

	others.NotifyAll(p)
}
