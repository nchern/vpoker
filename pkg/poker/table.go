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
func (r *Table) StartGame() *Table {
	id := 0
	for _, c := range r.Deck {
		r.Items = append(r.Items, NewTableItem(id, 0, 0).AsCard(c))
		id++
	}
	r.Shuffle()
	x := 10
	y := 20
	for i, c := range r.Chips {
		if i > 0 && r.Chips[i-1].Color != c.Color {
			x = 10
			y += 100
		}
		r.Items = append(r.Items, NewTableItem(id, x, y).AsChip(c))
		x++
		id++
	}
	r.Items = append(r.Items, NewTableItem(id, 470, 340).AsDealer())
	return r
}

func (r *Table) Shuffle() *Table {
	cards := r.Items[0:52]
	shuffle(cards)
	x := 150
	y := 20
	for _, it := range cards {
		it.X = x
		it.Y = y
		it.OwnerID = ""
		it.Side = Cover
		x++
	}
	return r
}

func (r *Table) generateChipsForPlayer(idx int) {
	// add chips
	slots := [][]int{
		{140, 610},
		{640, 20},
		{640, 640},
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
			item := NewTableItem(len(r.Items), x, y).AsChip(&ci)
			r.Items = append(r.Items, item)
			x += 2
		}
		x += chipWidth
	}
}

// Join joins a user
func (r *Table) Join(u *User) []*TableItem {
	index := len(r.Players) % len(PlayerColors)
	p := newPlayer(u, PlayerColors[index])
	p.Index = index
	p.Skin = fmt.Sprintf("player_%d", index)

	r.Players[u.ID] = p
	startIdx := len(r.Items)
	r.Items = append(r.Items, NewTableItem(len(r.Items), 0, 0).AsPlayer(p))

	r.generateChipsForPlayer(index)
	return r.Items[startIdx:]
}

// OtherPlayers returns all players but a given
func (r *Table) OtherPlayers(current *User) PlayerList {
	var others PlayerList
	for _, p := range r.Players {
		if p.ID == current.ID {
			continue
		}
		others = append(others, p)
	}
	return others
}

// DeepCopy creates a deep copy of this table via serialisation
func (r *Table) DeepCopy() (*Table, error) {
	var dest *Table
	b, err := json.Marshal(&r)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &dest); err != nil {
		return nil, err
	}
	return dest, nil
}

// ReadLock performs thread-safe read of this object
func (r *Table) ReadLock(fn func(*Table) error) error {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return fn(r)
}

// Update performs thread-safe update of this object
func (r *Table) Update(fn func(*Table) error) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	return fn(r)
}
