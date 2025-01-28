package models

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"

	"github.com/google/uuid"
	"github.com/nchern/vpoker/pkg/logger"
)

// User represents a system user
type User struct {
	ID   uuid.UUID
	Name string
}

// Ranks represents all possible card ranks
var Ranks = []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}

// Side is a card side
type Side string

const (
	Cover Side = "cover"
	Face  Side = "face"
)

// Suit is a card suit
type Suit string

const (
	Spades   Suit = "♠"
	Hearts   Suit = "♥"
	Diamonds Suit = "♦"
	Clubs    Suit = "♣"
)

type Card struct {
	Suit Suit   `json:"suit"`
	Rank string `json:"rank"`
	Side Side   `json:"side"`
}

// Color is a card color
type Color string

const (
	Red   Color = "red"
	Blue  Color = "blue"
	Gray  Color = "gray"
	Green Color = "green"
	Black Color = "black"
)

// color: #3498DB; Blue
// color: #F1C40F; Yellow
var (
	PlayerColors = []Color{
		"#FF5733", // Red
		"#9B59B6", // Purple
		"#2ECC71", // Green
	}
)

var chipTypes = map[Chip]int{
	{Color: Gray, Val: 1}:   10,
	{Color: Red, Val: 2}:    10,
	{Color: Blue, Val: 10}:  7,
	{Color: Green, Val: 25}: 2,
	{Color: Black, Val: 50}: 1,
}

// Chip represents a poker chip
type Chip struct {
	Color Color `json:"color"`
	Val   int   `json:"val"`
}

// Event represents an event that happens during the game
type Event string

const (
	UpdateAll Event = "update_all"
	// PlayerJoined Event = "player_joined"
)

type PlayerList []*Player

func (pl PlayerList) NotifyAll(e Event) {
	for _, p := range pl {
		// logger.Debug.Printf("recepient=%s send_push_begin", p.Name)
		p.Dispatch(e)
		// logger.Debug.Printf("recepient=%s send_push_finish", p.Name)
	}
}

// Player represents a player at the game table
type Player struct {
	*User

	// Color is an in game color of this player
	Color Color `json:"color"`

	// Skin represents a personalised style of this player
	Skin string `json:"skin"`

	// subscriptions map[uuid.UUID]chan Event
	updates chan Event
}

func newPlayer(u *User, c Color) *Player {
	return &Player{
		Color: c,
		User:  u,
	}
}

// Dispatch sends an update to this player
func (p *Player) Dispatch(update Event) *Player {
	defer func() {
		if r := recover(); r != nil {
			logger.Error.Println("Player.Dispatch panic:", r)
		}
	}()
	if p.updates != nil {
		p.updates <- update
	}
	return p
}

// Subscribe subscribes this player to async updates
func (p *Player) Subscribe(updates chan Event) *Player {
	if p.updates != nil {
		close(p.updates)
	}
	p.updates = updates
	return p
}

// Unsubscribe unsubscribes active update channel
func (p *Player) Unsubscribe() *Player {
	if p.updates != nil {
		close(p.updates)
		p.updates = nil
	}
	return p
}

// CardList is a list of cards
type CardList []*Card

// ShuffledCopy returns a shuffled copy of this list
func (li CardList) ShuffledCopy() CardList {
	shuffled := make([]*Card, len(li))
	copy(shuffled, li)
	// O(n)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rand.Intn(i)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled
}

// Room represents a poker room
type Room struct {
	// ID of this room
	ID uuid.UUID

	// Players represent players in this room
	Players map[uuid.UUID]*Player

	// Deck represents a deck of cards on the table
	Deck CardList

	// Chips represnets collection of all chips on the table
	Chips []*Chip

	// Items on the table
	Items TableItemList

	lock sync.RWMutex
}

// NewRoom creates a new Room instance
func NewRoom(id uuid.UUID, chipsN int) *Room {
	r := &Room{
		ID:      id,
		Players: map[uuid.UUID]*Player{},
	}
	for _, suit := range []Suit{Spades, Hearts, Diamonds, Clubs} {
		for _, rank := range Ranks {
			r.Deck = append(r.Deck, &Card{Rank: rank, Suit: suit, Side: Cover})
		}
	}
	for c, n := range chipTypes {
		for i := 0; i < n; i++ {
			r.Chips = append(r.Chips, &Chip{Val: c.Val, Color: c.Color})
		}
	}
	return r
}

// Start game rearaanges all the objects on the table to the initial state
func (r *Room) StartGame() *Room {
	id := 0
	x := 150
	y := 20
	for _, c := range r.Deck.ShuffledCopy() {
		r.Items = append(r.Items, NewTableItem(id, x, y).AsCard(c))
		x++
		id++
	}
	x = 10
	y = 20
	for i, c := range r.Chips {
		if i > 0 && r.Chips[i-1].Color != c.Color {
			y += 100
			x = 10
		}
		r.Items = append(r.Items, NewTableItem(id, x, y).AsChip(c))
		x++
		id++
	}
	r.Items = append(r.Items, NewTableItem(id, 180, 170).AsDealer())
	return r
}

// Join joins a user
func (r *Room) Join(u *User) *Room {
	n := len(r.Players) % len(PlayerColors)
	p := newPlayer(u, PlayerColors[n])
	p.Skin = fmt.Sprintf("player_%d", n)
	r.Players[u.ID] = p
	r.Items = append(r.Items, NewTableItem(len(r.Items), 0, 0).AsPlayer(p))
	return r
}

// Partners returns all partners of a given user, that is all players but a given one
func (r *Room) UpdatePartners(userID uuid.UUID, e Event) {
	others := []*Player{}
	r.lock.RLock()
	for _, p := range r.Players {
		if p.ID == userID {
			continue
		}
		others = append(others, p)
	}
	defer r.lock.RUnlock()

	for _, p := range others {
		// logger.Debug.Printf("recepient=%s send_push_begin", p.Name)
		p.Dispatch(e)
		// logger.Debug.Printf("recepient=%s send_push_finish", p.Name)
	}
}

// ReadLock performs thread-safe read of this object
func (r *Room) ReadLock(fn func(*Room) error) error {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return fn(r)
}

// Update performs thread-safe update of this object
func (r *Room) Update(fn func(*Room) error) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	return fn(r)
}

// Class represents a type of the item on the table
type Class string

const (
	CardClass   Class = "card"
	ChipClass   Class = "chip"
	DealerClass Class = "dealer"
	PlayerClass Class = "player"
)

// TableItemList is a list of TableItems
type TableItemList []*TableItem

// Get retrieves an item from the list by its index
func (l TableItemList) Get(i int) (*TableItem, error) {
	if i < 0 && i >= len(l) {
		return nil, errors.New("out of range")
	}
	return l[i], nil
}

// TableItem represents a virtual object on the table
type TableItem struct {
	Card
	Chip

	Class Class `json:"class"`

	OwnerID string `json:"owner_id"`

	ID int `json:"id"`
	X  int `json:"x"`
	Y  int `json:"y"`
}

// NewTableItem creates a new table item
func NewTableItem(id int, x int, y int) *TableItem {
	return &TableItem{
		ID: id,
		X:  x,
		Y:  y,
	}
}

// AsCard makes this item as card
func (ti *TableItem) AsCard(c *Card) *TableItem {
	ti.Card = *c
	ti.Class = CardClass
	return ti
}

// AsChip makes this item as chip
func (ti *TableItem) AsChip(c *Chip) *TableItem {
	ti.Chip = *c
	ti.Class = ChipClass
	return ti
}

// AsDealer makes this item as dealer chip
func (ti *TableItem) AsDealer() *TableItem {
	ti.Class = DealerClass
	return ti
}

// AsPlayer makes this item as player object
func (ti *TableItem) AsPlayer(p *Player) *TableItem {
	ti.OwnerID = p.ID.String()
	ti.Class = PlayerClass
	return ti
}

// Is defines if this item belongs to a specified class
func (ti *TableItem) Is(cls Class) bool { return ti.Class == cls }
