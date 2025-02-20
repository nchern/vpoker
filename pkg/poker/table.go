package poker

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/nchern/vpoker/pkg/httpx"
	"github.com/nchern/vpoker/pkg/logger"
)

const maxPlayers = 3

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

	// idSeq is responsible for items id generation
	idSeq sequence
}

type sequence int

func (s *sequence) Next() int {
	(*s)++
	return int(*s)

}

// NewTable creates a new table instance
func NewTable(id uuid.UUID, chipsN int) *Table {
	r := &Table{
		idSeq:   0,
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
	logger.Debug.Printf("StartGame begin table_id=%s seq=%d", t.ID, int(t.idSeq))
	for _, c := range t.Deck {
		t.Items = append(t.Items, NewTableItem(t.idSeq.Next(), 0, 0).AsCard(c))
	}
	t.Shuffle()
	x := 10
	y := 20
	for i, c := range t.Chips {
		if i > 0 && t.Chips[i-1].Color != c.Color {
			x = 10
			y += 100
		}
		t.Items = append(t.Items,
			NewTableItem(t.idSeq.Next(), x, y).AsChip(c))
		x++
	}
	t.Items = append(t.Items, NewTableItem(t.idSeq.Next(), 595, 315).AsDealer())
	logger.Debug.Printf("StartGame finished table_id=%s seq=%d", t.ID, int(t.idSeq))
	return t
}

// Shuffle shuffles cards on the table
func (t *Table) Shuffle() *Table {
	cards := t.Items[0:52]
	shuffle(cards)
	x := 150
	y := 20
	for _, it := range cards {
		it.X = x
		it.Y = y
		it.ZIndex = 1
		it.OwnerID = ""
		it.Side = Cover
		it.PrevOwnerID = ""
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
			item := NewTableItem(t.idSeq.Next(), x, y).AsChip(&ci)
			t.Items = append(t.Items, item)
			x += 2
		}
		x += chipWidth
	}
}

// Join joins a user
func (t *Table) Join(u *User) ([]*TableItem, error) {
	if len(t.Players) >= maxPlayers {
		return nil, httpx.NewError(http.StatusForbidden, "this table is full")
	}
	index := -1
	var slots [maxPlayers]byte
	// player's index is always between 0 and maxPlayers excluding
	// O(maxPlayers) which is really small O as the order of magnitude of maxPlayers is not more than 10
	for _, p := range t.Players {
		slots[p.Index] = 1
	}
	for i := 0; i < len(slots); i++ {
		if slots[i] == 0 {
			index = i
			break
		}
	}
	if index < 0 {
		// this is a safeguard, should never happen
		return nil, httpx.NewError(http.StatusForbidden, "this table is full")
	}
	logger.Debug.Printf("Join begin table_id=%s seq=%d", t.ID, int(t.idSeq))
	p := newPlayer(u, playerColors[index])
	p.Index = index
	p.Skin = fmt.Sprintf("player_%d", index)

	t.Players[u.ID] = p
	startIdx := len(t.Items)
	logger.Debug.Printf("Join: user_name=%s start_id=%d", u.Name, int(t.idSeq))
	t.Items = append(t.Items, NewTableItem(t.idSeq.Next(), 0, 0).AsPlayer(p))

	t.generateChipsForPlayer(index)
	logger.Debug.Printf("Join finished table_id=%s seq=%d", t.ID, int(t.idSeq))
	return t.Items[startIdx:], nil
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

// KickPlayer kicks a player from this table by name
func (t *Table) KickPlayer(name string) error {
	// XXX: Currently O(n). Should not be a problem at least for a while as
	// the number of items on the table should not exceed 1000 which is fine
	// to process sequentially
	var player *Player
	for _, p := range t.Players {
		if p.Name == name {
			player = p
			break
		}
	}
	if player == nil {
		return httpx.NewError(http.StatusBadRequest, "user is not at the table")
	}
	for i, it := range t.Items {
		// TODO: optimize
		if it.Class == PlayerClass && it.OwnerID == player.ID.String() {
			t.Items = append(t.Items[0:i], t.Items[i+1:]...)
			break
		}
	}
	for _, p := range t.Players {
		p.Dispatch(NewPushPlayerKicked(player))
	}
	delete(t.Players, player.ID)
	player.Unsubscribe()
	return nil
}
