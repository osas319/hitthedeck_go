package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── Constants ──
const (
	TickRate   = 20
	MapSize    = 1600
	WalkSpeed  = 0.6
	JumpVel    = 0.4
	Gravity    = 0.03
	DrownTime  = 4000
	EmbarkDist = 18
	ShoreDist  = 14
	CBLife     = 2500
	CBDmg      = 18
	StartGold  = 5000
)

// ── Ship definitions ──
type ShipDef struct {
	Name    string  `json:"name"`
	Price   int     `json:"price"`
	HP      int     `json:"hp"`
	Speed   float64 `json:"speed"`
	Turn    float64 `json:"turn"`
	Cannons string  `json:"cannons"`
	Count   int     `json:"count"`
	Reload  int64   `json:"reload"`
	Cargo   int     `json:"cargo"`
}

var Ships = map[string]ShipDef{
	"rowboat":   {Name: "Rowboat", Price: 0, HP: 80, Speed: 0.85, Turn: 0.04, Cannons: "front", Count: 1, Reload: 1200, Cargo: 0},
	"warship":   {Name: "War Galleon", Price: 2000, HP: 200, Speed: 0.6, Turn: 0.025, Cannons: "side", Count: 3, Reload: 2500, Cargo: 0},
	"tradeship": {Name: "Trade Schooner", Price: 1500, HP: 130, Speed: 0.75, Turn: 0.032, Cannons: "front", Count: 1, Reload: 1000, Cargo: 1000},
}

type GoodDef struct {
	Name string `json:"name"`
	Size int    `json:"size"`
	Icon string `json:"icon"`
}

var Goods = map[string]GoodDef{
	"coffee": {Name: "Coffee", Size: 15, Icon: "☕"},
	"spice":  {Name: "Spice", Size: 20, Icon: "🌶"},
	"beer":   {Name: "Beer", Size: 30, Icon: "🍺"},
}

type OreDef struct {
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Value int    `json:"value"`
}

var Ores = map[string]OreDef{
	"iron":   {Name: "Iron", Icon: "⛏", Value: 50},
	"gold":   {Name: "Gold", Icon: "🥇", Value: 150},
	"bronze": {Name: "Bronze", Icon: "🔶", Value: 80},
}

// ── World ──
type Island struct {
	X     float64            `json:"x"`
	Z     float64            `json:"z"`
	R     float64            `json:"r"`
	Name  string             `json:"name"`
	Goods map[string]*IGood  `json:"goods,omitempty"`
	Ore   string             `json:"ore,omitempty"`
}

type IGood struct {
	Price int `json:"price"`
	Stock int `json:"stock"`
}

type Rock struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
	R float64 `json:"r"`
}

var tradeIslands = []*Island{
	{X: -400, Z: -400, R: 80, Name: "Skull Isle", Goods: map[string]*IGood{"coffee": {40, 100}, "beer": {25, 80}}},
	{X: 400, Z: -400, R: 90, Name: "Palm Haven", Goods: map[string]*IGood{"spice": {55, 60}, "coffee": {50, 70}}},
	{X: 400, Z: 400, R: 75, Name: "Fort Rock", Goods: map[string]*IGood{"beer": {30, 90}, "spice": {45, 50}}},
	{X: -400, Z: 400, R: 85, Name: "Treasure Cove", Goods: map[string]*IGood{"coffee": {35, 80}, "spice": {60, 40}, "beer": {20, 100}}},
}

var mineIslands = []*Island{
	{X: 0, Z: -300, R: 22, Name: "Iron Reef", Ore: "iron"},
	{X: -200, Z: 0, R: 18, Name: "Gold Shoal", Ore: "gold"},
	{X: 200, Z: 0, R: 20, Name: "Bronze Atoll", Ore: "bronze"},
	{X: 0, Z: 300, R: 18, Name: "Copper Cay", Ore: "iron"},
	{X: -150, Z: -200, R: 15, Name: "Nugget Isle", Ore: "gold"},
	{X: 150, Z: 200, R: 16, Name: "Tin Rock", Ore: "bronze"},
}

var allIslands []*Island
var rocks []Rock

func initWorld() {
	allIslands = append(allIslands, tradeIslands...)
	allIslands = append(allIslands, mineIslands...)
	for _, il := range allIslands {
		n := int(il.R / 10)
		for i := 0; i < n; i++ {
			a := rand.Float64() * math.Pi * 2
			d := 5 + rand.Float64()*(il.R-12)
			rocks = append(rocks, Rock{X: il.X + math.Cos(a)*d, Z: il.Z + math.Sin(a)*d, R: 1.5 + rand.Float64()*1.5})
		}
	}
}

func onLand(x, z float64) *Island {
	for _, il := range allIslands {
		if math.Hypot(x-il.X, z-il.Z) < il.R { return il }
	}
	return nil
}

func nearShore(x, z float64) *Island {
	for _, il := range allIslands {
		d := math.Hypot(x-il.X, z-il.Z)
		if d < il.R+ShoreDist && d > il.R-8 { return il }
	}
	return nil
}

func hitRock(x, z, r float64) *Rock {
	for i := range rocks {
		if math.Hypot(x-rocks[i].X, z-rocks[i].Z) < rocks[i].R+r { return &rocks[i] }
	}
	return nil
}

func isMine(il *Island) bool {
	for _, m := range mineIslands {
		if m == il { return true }
	}
	return false
}

func spawnPos() (float64, float64) {
	for {
		x := (rand.Float64() - 0.5) * MapSize * 0.5
		z := (rand.Float64() - 0.5) * MapSize * 0.5
		if onLand(x, z) == nil { return x, z }
	}
}

// ── Player ──
type Input struct {
	Fwd   bool `json:"fwd"`
	Back  bool `json:"back"`
	Left  bool `json:"left"`
	Right bool `json:"right"`
	Act   bool `json:"act"`
	Jump  bool `json:"jump"`
}

type Player struct {
	ID   string
	Name string
	Conn *websocket.Conn
	mu   sync.Mutex

	CX, CZ, CY, CR float64
	CVY             float64
	BX, BZ, BR, BS  float64
	OnBoat, Swim    bool
	SwimT           int64

	Ship    string
	Owned   []string
	Gold    int
	HP, MHP int
	Score   int
	Sails   int
	Alive   bool

	Inp     Input
	LastF   int64
	AX, AZ  float64
	ActP    bool
	JoinT   int64

	Cargo     map[string]int
	CargoUsed int
	Inv       map[string]int
	Mining    bool
	MineT     int64
}

func (p *Player) Send(msg interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	p.Conn.WriteJSON(msg)
}

// ── Cannonball ──
type CB struct {
	X, Z, Y    float64
	VX, VZ, VY float64
	Owner      string
	Born       int64
}

// ── Ground Item ──
type GItem struct {
	ID   int     `json:"id"`
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	Type string  `json:"type"`
}

// ── Game ──
type Game struct {
	mu      sync.RWMutex
	players map[string]*Player
	cbs     []*CB
	items   []GItem
	itemID  int
}

func NewGame() *Game {
	return &Game{players: make(map[string]*Player)}
}

func now() int64 { return time.Now().UnixMilli() }

// ── State broadcast format ──
type PState struct {
	CX float64 `json:"cx"` ; CZ float64 `json:"cz"` ; CY float64 `json:"cy"` ; CR float64 `json:"cr"`
	BX float64 `json:"bx"` ; BZ float64 `json:"bz"` ; BR float64 `json:"br"` ; BS float64 `json:"bs"`
	SL int     `json:"sl"` ; OB bool    `json:"ob"` ; SW bool    `json:"sw"`
	HP int     `json:"hp"` ; MHP int    `json:"mhp"`; SC int     `json:"sc"` ; G int `json:"g"`
	N  string  `json:"n"`  ; SH string  `json:"sh"` ; AX float64 `json:"ax"` ; AZ float64 `json:"az"`
	Cargo map[string]int `json:"cargo"` ; CU int `json:"cu"`
	Inv   map[string]int `json:"inv"`   ; MN bool `json:"mn"`
}

type CBState struct {
	X float64 `json:"x"` ; Z float64 `json:"z"` ; Y float64 `json:"y"`
}

type FullState struct {
	T  int64              `json:"t"`
	P  map[string]*PState `json:"p"`
	CB []CBState          `json:"cb"`
	GI []GItem            `json:"gi"`
}

func (g *Game) buildState() *FullState {
	s := &FullState{T: now(), P: make(map[string]*PState), GI: g.items}
	s.CB = make([]CBState, len(g.cbs))
	for i, c := range g.cbs { s.CB[i] = CBState{c.X, c.Z, c.Y} }
	for id, p := range g.players {
		s.P[id] = &PState{
			CX: p.CX, CZ: p.CZ, CY: p.CY, CR: p.CR,
			BX: p.BX, BZ: p.BZ, BR: p.BR, BS: p.BS,
			SL: p.Sails, OB: p.OnBoat, SW: p.Swim,
			HP: p.HP, MHP: p.MHP, SC: p.Score, G: p.Gold,
			N: p.Name, SH: p.Ship, AX: p.AX, AZ: p.AZ,
			Cargo: p.Cargo, CU: p.CargoUsed, Inv: p.Inv, MN: p.Mining,
		}
	}
	if s.GI == nil { s.GI = []GItem{} }
	return s
}

func (g *Game) respawn(p *Player) {
	x, z := spawnPos()
	p.BX, p.BZ = x, z
	p.BR = rand.Float64() * math.Pi * 2
	p.BS = 0
	p.CX, p.CZ, p.CY, p.CR = x, z, 0, p.BR
	p.OnBoat, p.Swim, p.Mining = true, false, false
	sh := Ships[p.Ship]
	p.HP, p.MHP = sh.HP, sh.HP
	p.Sails = 1
	p.Alive = true
}

// ── Fire ──
func (g *Game) fire(p *Player) {
	sh := Ships[p.Ship]
	dx, dz := p.AX-p.BX, p.AZ-p.BZ
	dist := math.Max(20, math.Min(180, math.Hypot(dx, dz)))
	if sh.Cannons == "front" {
		g.mkCB(p, p.BX+math.Sin(p.BR)*6, p.BZ+math.Cos(p.BR)*6, p.AX, p.AZ, dist)
	} else {
		aim := math.Atan2(dx, dz)
		rel := aim - p.BR
		for rel > math.Pi { rel -= math.Pi * 2 }
		for rel < -math.Pi { rel += math.Pi * 2 }
		side := 1.0
		if rel <= 0 { side = -1.0 }
		sa := p.BR + side*math.Pi/2
		for i := 0; i < sh.Count; i++ {
			off := float64(i-1) * 4
			ox := p.BX + math.Sin(p.BR)*off + math.Sin(sa)*5
			oz := p.BZ + math.Cos(p.BR)*off + math.Cos(sa)*5
			sp := float64(i-1) * 7
			g.mkCB(p, ox, oz, p.AX+math.Sin(p.BR)*sp, p.AZ+math.Cos(p.BR)*sp, dist)
		}
	}
}

func (g *Game) mkCB(p *Player, sx, sz, tx, tz, dist float64) {
	d := math.Max(1, math.Hypot(tx-sx, tz-sz))
	spd := 2.8
	fl := dist / spd / 20
	g.cbs = append(g.cbs, &CB{X: sx, Z: sz, Y: 3.5,
		VX: (tx - sx) / d * spd, VZ: (tz - sz) / d * spd, VY: fl * 0.45,
		Owner: p.ID, Born: now()})
}

// ── Game tick ──
func (g *Game) tick() {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := now()

	for _, p := range g.players {
		if !p.Alive { continue }
		inp := p.Inp
		sh := Ships[p.Ship]

		if p.OnBoat {
			if inp.Left  { p.BR += sh.Turn }
			if inp.Right { p.BR -= sh.Turn }
			mx := []float64{0, sh.Speed * 0.45, sh.Speed}[p.Sails]
			if inp.Fwd      { p.BS = math.Min(p.BS+0.04, mx) } else
			if inp.Back     { p.BS = math.Max(p.BS-0.04, -mx*0.2) } else
			if p.Sails == 0 { p.BS *= 0.95 } else { p.BS += (mx*0.7 - p.BS) * 0.008 }

			p.BX += math.Sin(p.BR) * p.BS
			p.BZ += math.Cos(p.BR) * p.BS
			h := float64(MapSize) / 2
			p.BX = math.Max(-h, math.Min(h, p.BX))
			p.BZ = math.Max(-h, math.Min(h, p.BZ))

			for _, il := range allIslands {
				d := math.Hypot(p.BX-il.X, p.BZ-il.Z)
				m := il.R + 6
				if d < m {
					a := math.Atan2(p.BX-il.X, p.BZ-il.Z)
					p.BX = il.X + math.Sin(a)*m
					p.BZ = il.Z + math.Cos(a)*m
					p.BS *= 0.2
				}
			}
			p.CX, p.CZ, p.CR = p.BX, p.BZ, p.BR

			if inp.Act && !p.ActP {
				p.ActP = true
				if shore := nearShore(p.BX, p.BZ); shore != nil {
					a := math.Atan2(p.BX-shore.X, p.BZ-shore.Z)
					p.CX = shore.X + math.Sin(a)*(shore.R-4)
					p.CZ = shore.Z + math.Cos(a)*(shore.R-4)
					p.CY, p.CR = 3, a
					p.OnBoat, p.Swim, p.Mining = false, false, false
				}
			}
			if !inp.Act { p.ActP = false }
		} else {
			// On foot
			if inp.Left  { p.CR += 0.06 }
			if inp.Right { p.CR -= 0.06 }
			var mx, mz float64
			if inp.Fwd  { mx = math.Sin(p.CR) * WalkSpeed; mz = math.Cos(p.CR) * WalkSpeed }
			if inp.Back { mx = -math.Sin(p.CR) * WalkSpeed * 0.5; mz = -math.Cos(p.CR) * WalkSpeed * 0.5 }

			nx, nz := p.CX+mx, p.CZ+mz
			if ob := hitRock(nx, nz, 1); ob == nil {
				p.CX, p.CZ = nx, nz
			} else {
				a := math.Atan2(nx-ob.X, nz-ob.Z)
				p.CX = ob.X + math.Sin(a)*(ob.R+1.2)
				p.CZ = ob.Z + math.Cos(a)*(ob.R+1.2)
			}

			if inp.Jump && p.CY <= 3.05 && !p.Swim { p.CVY = JumpVel }
			p.CVY -= Gravity
			p.CY += p.CVY
			land := onLand(p.CX, p.CZ)
			gy := -0.5
			if land != nil { gy = 3 }
			if p.CY < gy { p.CY = gy; p.CVY = 0 }

			if land == nil {
				if !p.Swim { p.Swim = true; p.SwimT = n }
				if n-p.SwimT > DrownTime {
					p.HP -= 60
					if p.HP <= 0 {
						g.respawn(p)
						p.Send(map[string]interface{}{"t": "sunk", "by": "the sea"})
					} else {
						p.OnBoat, p.Swim = true, false
						p.CX, p.CZ, p.CY = p.BX, p.BZ, 0
					}
				}
			} else {
				p.Swim = false
				// Mining
				if p.Mining && isMine(land) && n-p.MineT > 2000 {
					p.Mining = false
					a := rand.Float64() * math.Pi * 2
					g.itemID++
					g.items = append(g.items, GItem{ID: g.itemID, X: p.CX + math.Cos(a)*3, Z: p.CZ + math.Sin(a)*3, Type: land.Ore})
				}
			}

			// Auto-pickup
			for i := len(g.items) - 1; i >= 0; i-- {
				gi := g.items[i]
				if math.Hypot(p.CX-gi.X, p.CZ-gi.Z) < 3 {
					p.Inv[gi.Type]++
					g.items = append(g.items[:i], g.items[i+1:]...)
				}
			}

			if inp.Act && !p.ActP {
				p.ActP = true
				if math.Hypot(p.CX-p.BX, p.CZ-p.BZ) < EmbarkDist {
					p.OnBoat, p.Swim, p.Mining = true, false, false
					p.CX, p.CZ, p.CY = p.BX, p.BZ, 0
				}
			}
			if !inp.Act { p.ActP = false }
		}
	}

	// Cannonballs
	for i := len(g.cbs) - 1; i >= 0; i-- {
		c := g.cbs[i]
		c.X += c.VX; c.Z += c.VZ; c.Y += c.VY; c.VY -= 0.045
		if c.Y < 0 || n-c.Born > CBLife {
			g.cbs = append(g.cbs[:i], g.cbs[i+1:]...)
			continue
		}
		for id, p := range g.players {
			if id == c.Owner || !p.Alive { continue }
			if math.Hypot(c.X-p.BX, c.Z-p.BZ) < 10 {
				p.HP -= CBDmg
				g.cbs = append(g.cbs[:i], g.cbs[i+1:]...)
				if p.HP <= 0 {
					if k, ok := g.players[c.Owner]; ok {
						k.Score++; k.Gold += 200
					}
					p.Send(map[string]interface{}{"t": "sunk", "by": func() string {
						if k, ok := g.players[c.Owner]; ok { return k.Name }
						return "?"
					}()})
					g.respawn(p)
				}
				break
			}
		}
	}

	// Broadcast state
	state := g.buildState()
	data, _ := json.Marshal(map[string]interface{}{"t": "s", "d": state})
	for _, p := range g.players {
		p.mu.Lock()
		p.Conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		p.Conn.WriteMessage(websocket.TextMessage, data)
		p.mu.Unlock()
	}
}

// ── WebSocket handler ──
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (g *Game) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { log.Println("Upgrade err:", err); return }
	defer conn.Close()

	id := fmt.Sprintf("p_%d_%d", now(), rand.Intn(9999))
	sx, sz := spawnPos()
	sr := rand.Float64() * math.Pi * 2

	p := &Player{
		ID: id, Name: "Pirate", Conn: conn,
		CX: sx, CZ: sz, BX: sx, BZ: sz, BR: sr, AX: sx, AZ: sz + 30,
		OnBoat: true, Ship: "rowboat", Owned: []string{"rowboat"},
		Gold: StartGold, HP: 80, MHP: 80, Sails: 1, Alive: true, JoinT: now(),
		Cargo: make(map[string]int), Inv: make(map[string]int),
	}

	g.mu.Lock()
	g.players[id] = p
	state := g.buildState()
	g.mu.Unlock()

	// Send init
	p.Send(map[string]interface{}{
		"t": "init", "id": id, "islands": tradeIslands, "mineIslands": mineIslands,
		"rocks": rocks, "map": MapSize, "ships": Ships, "goods": Goods, "ores": Ores,
		"state": state, "spawnX": sx, "spawnZ": sz, "spawnR": sr,
	})

	log.Printf("+ %s (%d players)", id, len(g.players))

	// Read loop
	for {
		_, data, err := conn.ReadMessage()
		if err != nil { break }

		var msg map[string]json.RawMessage
		if json.Unmarshal(data, &msg) != nil { continue }

		t := ""
		json.Unmarshal(msg["t"], &t)

		g.mu.Lock()
		switch t {
		case "input":
			var inp Input
			json.Unmarshal(data, &inp) // flatten parse
			// Re-parse properly
			type inputMsg struct { Fwd bool `json:"fwd"`; Back bool `json:"back"`; Left bool `json:"left"`; Right bool `json:"right"`; Act bool `json:"act"`; Jump bool `json:"jump"` }
			var im inputMsg
			json.Unmarshal(data, &im)
			p.Inp = Input{Fwd: im.Fwd, Back: im.Back, Left: im.Left, Right: im.Right, Act: im.Act, Jump: im.Jump}

		case "aim":
			type aimMsg struct { X float64 `json:"x"`; Z float64 `json:"z"` }
			var am aimMsg
			json.Unmarshal(data, &am)
			p.AX, p.AZ = am.X, am.Z

		case "fire":
			if p.OnBoat && p.Alive {
				sh := Ships[p.Ship]
				if now()-p.LastF >= int64(sh.Reload) {
					p.LastF = now()
					g.fire(p)
				}
			}

		case "setSails":
			var v struct{ V int `json:"v"` }
			json.Unmarshal(data, &v)
			if v.V >= 0 && v.V <= 2 { p.Sails = v.V }

		case "setName":
			var v struct{ V string `json:"v"` }
			json.Unmarshal(data, &v)
			if len(v.V) > 0 && len(v.V) <= 16 { p.Name = v.V }

		case "buy":
			var v struct{ V string `json:"v"` }
			json.Unmarshal(data, &v)
			if sh, ok := Ships[v.V]; ok {
				owned := false
				for _, o := range p.Owned { if o == v.V { owned = true; break } }
				if owned {
					p.Send(map[string]interface{}{"t": "msg", "v": "Already owned!"})
				} else if p.Gold < sh.Price {
					p.Send(map[string]interface{}{"t": "msg", "v": "Not enough gold!"})
				} else {
					p.Gold -= sh.Price
					p.Owned = append(p.Owned, v.V)
					p.Send(map[string]interface{}{"t": "msg", "v": "Bought " + sh.Name + "!"})
					p.Send(map[string]interface{}{"t": "upd", "gold": p.Gold, "owned": p.Owned})
				}
			}

		case "equip":
			var v struct{ V string `json:"v"` }
			json.Unmarshal(data, &v)
			owned := false
			for _, o := range p.Owned { if o == v.V { owned = true; break } }
			if owned {
				p.Ship = v.V
				sh := Ships[v.V]
				p.MHP, p.HP = sh.HP, sh.HP
				p.Cargo = make(map[string]int)
				p.CargoUsed = 0
				p.Send(map[string]interface{}{"t": "upd", "ship": v.V, "hp": p.HP, "mhp": p.MHP})
			}

		case "tp":
			type tpMsg struct{ X float64 `json:"x"`; Z float64 `json:"z"` }
			var tm tpMsg
			json.Unmarshal(data, &tm)
			p.BX, p.BZ, p.CX, p.CZ, p.BS = tm.X, tm.Z, tm.X, tm.Z, 0
			if !p.OnBoat { p.OnBoat, p.Swim = true, false }

		case "buyGood":
			type bgMsg struct{ Good string `json:"good"`; Qty int `json:"qty"`; Idx int `json:"idx"` }
			var bm bgMsg
			json.Unmarshal(data, &bm)
			if bm.Idx >= 0 && bm.Idx < len(tradeIslands) && !p.OnBoat {
				isl := tradeIslands[bm.Idx]
				if ig, ok := isl.Goods[bm.Good]; ok {
					if gd, ok := Goods[bm.Good]; ok {
						cap := Ships[p.Ship].Cargo
						if cap == 0 { p.Send(map[string]interface{}{"t": "msg", "v": "No cargo!"}); break }
						max := bm.Qty
						if ig.Stock < max { max = ig.Stock }
						if avail := (cap - p.CargoUsed) / gd.Size; avail < max { max = avail }
						if max <= 0 { break }
						cost := max * ig.Price
						if p.Gold < cost { p.Send(map[string]interface{}{"t": "msg", "v": "Not enough gold!"}); break }
						p.Gold -= cost
						ig.Stock -= max
						p.Cargo[bm.Good] += max
						p.CargoUsed += max * gd.Size
						p.Send(map[string]interface{}{"t": "msg", "v": fmt.Sprintf("Bought %d %s", max, gd.Name)})
					}
				}
			}

		case "sellGood":
			type sgMsg struct{ Good string `json:"good"`; Qty int `json:"qty"`; Idx int `json:"idx"` }
			var sm sgMsg
			json.Unmarshal(data, &sm)
			if sm.Idx >= 0 && sm.Idx < len(tradeIslands) && !p.OnBoat {
				isl := tradeIslands[sm.Idx]
				if ig, ok := isl.Goods[sm.Good]; ok {
					have := p.Cargo[sm.Good]
					sell := sm.Qty
					if have < sell { sell = have }
					if sell <= 0 { break }
					price := int(float64(ig.Price) * 1.3)
					p.Gold += sell * price
					p.Cargo[sm.Good] -= sell
					if p.Cargo[sm.Good] <= 0 { delete(p.Cargo, sm.Good) }
					p.CargoUsed -= sell * Goods[sm.Good].Size
					p.Send(map[string]interface{}{"t": "msg", "v": fmt.Sprintf("Sold %d for %dg", sell, sell*price)})
				}
			}

		case "sellOre":
			type soMsg struct{ Ore string `json:"ore"`; Qty int `json:"qty"` }
			var sm soMsg
			json.Unmarshal(data, &sm)
			if od, ok := Ores[sm.Ore]; ok && !p.OnBoat {
				have := p.Inv[sm.Ore]
				sell := sm.Qty
				if have < sell { sell = have }
				if sell > 0 {
					p.Gold += sell * od.Value
					p.Inv[sm.Ore] -= sell
					if p.Inv[sm.Ore] <= 0 { delete(p.Inv, sm.Ore) }
					p.Send(map[string]interface{}{"t": "msg", "v": fmt.Sprintf("Sold %d %s for %dg", sell, od.Name, sell*od.Value)})
				}
			}

		case "mine":
			if !p.OnBoat && !p.Mining {
				if land := onLand(p.CX, p.CZ); land != nil && isMine(land) {
					p.Mining = true
					p.MineT = now()
				}
			}
		}
		g.mu.Unlock()
	}

	// Disconnect
	g.mu.Lock()
	delete(g.players, id)
	g.mu.Unlock()

	// Notify others
	g.mu.RLock()
	data, _ := json.Marshal(map[string]interface{}{"t": "left", "id": id})
	for _, op := range g.players {
		op.mu.Lock()
		op.Conn.WriteMessage(websocket.TextMessage, data)
		op.mu.Unlock()
	}
	g.mu.RUnlock()
	log.Printf("- %s", id)
}

// ── Main ──
func main() {
	rand.Seed(time.Now().UnixNano())
	initWorld()

	game := NewGame()

	// Game loop
	go func() {
		ticker := time.NewTicker(time.Second / TickRate)
		for range ticker.C {
			game.tick()
		}
	}()

	http.Handle("/", http.FileServer(http.Dir("public")))
	http.HandleFunc("/ws", game.handleWS)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) })

	port := os.Getenv("PORT")
	if port == "" { port = "3000" }
	log.Printf("Krew3D Go server on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
