package poker

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nchern/vpoker/pkg/logger"
)

const (
	chipWidth = 70
)

var (
	// color: #3498DB; Blue
	// color: #F1C40F; Yellow
	PlayerColors = []Color{
		"#FF5733", // Red
		"#9B59B6", // Purple
		"#2ECC71", // Green
	}
)

// User represents a system user
type User struct {
	CreatedAt time.Time

	ID uuid.UUID

	Name string
}

// NewUser creates a new instance of a User
func NewUser(iD uuid.UUID, name string, createdAt time.Time) *User {
	return &User{
		CreatedAt: createdAt,
		ID:        iD,
		Name:      name,
	}
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
	BlankSuit      = ""
	Spades    Suit = "♠"
	Hearts    Suit = "♥"
	Diamonds  Suit = "♦"
	Clubs     Suit = "♣"
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

var chipsSet = []Chip{
	{Color: Gray, Val: 1},
	{Color: Red, Val: 5},
	{Color: Blue, Val: 10},
	{Color: Green, Val: 25},
	{Color: Black, Val: 50},
}

// Chip represents a poker chip
type Chip struct {
	Color Color `json:"color"`
	Val   int   `json:"val"`
}

// PushType represents a push type
type PushType string

const (
	Refresh      PushType = "refresh"
	PlayerJoined PushType = "player_joined"
	UpdateItems  PushType = "update_items"
)

// Push represents a push event that happens in the game and
// carries objects to push to a client
type Push struct {
	Type PushType `json:"type"`

	Items []*TableItem `json:"items"`

	Players map[uuid.UUID]*Player `json:"players"`
}

// DeepCopy creates a deep copy of this push via serialisation
func (p *Push) DeepCopy() (*Push, error) {
	var dest *Push
	b, err := json.Marshal(&p)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &dest); err != nil {
		return nil, err
	}
	return dest, nil
}

func NewPushItems(items ...*TableItem) *Push {
	return &Push{Type: UpdateItems, Items: items}
}

func NewPushPlayerJoined(players map[uuid.UUID]*Player, items ...*TableItem) *Push {
	return &Push{
		Type: PlayerJoined,

		Items:   items,
		Players: players,
	}
}

func NewPushRefresh() *Push { return &Push{Type: Refresh} }

type PlayerList []*Player

func (pl PlayerList) NotifyAll(push *Push) {
	for _, p := range pl {
		// logger.Debug.Printf("recepient=%s send_push_begin", p.Name)
		p.Dispatch(push)
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

	// Index represents player index in slots
	Index int `json:"index"`

	updates chan *Push
}

func newPlayer(u *User, c Color) *Player {
	return &Player{
		Color: c,
		User:  u,
	}
}

// Dispatch sends an update to this player
func (p *Player) Dispatch(push *Push) *Player {
	defer func() {
		if r := recover(); r != nil {
			logger.Error.Printf("Player.Dispatch name=%s panic: %s", p.Name, r)
		}
	}()
	if p.updates != nil {
		p.updates <- push
	}
	return p
}

// Subscribe subscribes this player to async updates
func (p *Player) Subscribe(updates chan *Push) *Player {
	if p.updates != nil {
		defer func() {
			if r := recover(); r != nil {
				logger.Error.Printf("Player.Subscribe name=%s panic: %s", p.Name, r)
			}
		}()
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

// Room represents a poker room
type Room struct {
	// ID of this room
	ID uuid.UUID `json:"id"`

	// Players represent players in this room
	Players map[uuid.UUID]*Player `json:"players"`

	// Deck represents a deck of cards on the table
	Deck CardList `json:"-"`

	// Chips represnets collection of all chips on the table
	Chips []*Chip `json:"-"`

	// Items on the table
	Items TableItemList `json:"items"`

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
func (r *Room) StartGame() *Room {
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

func (r *Room) Shuffle() *Room {
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

func (r *Room) generateChipsForPlayer(idx int) {
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
func (r *Room) Join(u *User) []*TableItem {
	index := len(r.Players) % len(PlayerColors)
	p := newPlayer(u, PlayerColors[index])
	p.Index = index
	p.Skin = fmt.Sprintf("player_%d", index)

	r.Players[u.ID] = p
	r.Items = append(r.Items, NewTableItem(len(r.Items), 0, 0).AsPlayer(p))

	r.generateChipsForPlayer(index)
	startIdx := len(r.Items)
	return r.Items[startIdx:]
}

// OtherPlayers returns all players but a given
func (r *Room) OtherPlayers(current *User) PlayerList {
	var others PlayerList
	for _, p := range r.Players {
		if p.ID == current.ID {
			continue
		}
		others = append(others, p)
	}
	return others
}

// DeepCopy creates a deep copy of this room via serialisation
func (r *Room) DeepCopy() (*Room, error) {
	var dest *Room
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

// Get retrieves an item from the list by its id
// XXX: O(n) implementation
// TODO: make lookups O(1)
func (l TableItemList) Get(id int) *TableItem {
	for _, it := range l {
		if it.ID == id {
			return it
		}
	}
	return nil
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

// IsOwnedBy checks if this item is owned by a specified user
func (ti *TableItem) IsOwnedBy(id uuid.UUID) bool {
	return ti.OwnerID == id.String()
}

// IsOwned checks if this item is owned by anyone
func (ti *TableItem) IsOwned() bool { return ti.OwnerID != "" }

// ApplyVisibilityRules evaluates visibility for fields of this item
// Currently it works for cards only preventing non owners to obtain
// information about card rank and suit.
func (ti *TableItem) ApplyVisibilityRules(curUser *User) {
	if !ti.Is(CardClass) {
		return // do nothing if this is not a card
	}
	if ti.IsOwnedBy(curUser.ID) {
		ti.Side = Face // owners always see their cards
	}
	isOwnedBySomeoneElse := ti.IsOwned() && !ti.IsOwnedBy(curUser.ID)
	if isOwnedBySomeoneElse {
		ti.Side = Cover // if a card is owned by someone, others always see their card cover
	}
	if ti.Side == Cover {
		ti.Rank = ""
		ti.Suit = BlankSuit
	}
}
