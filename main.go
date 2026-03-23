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

const (
	TICK=20; MAP=1600; WALK=0.6; JUMP=0.4; GRAV=0.03
	DROWN=4000; EMBARK=18; SHORE=14; CB_LIFE=2500; CB_DMG=18; GOLD0=5000
)

type ShipDef struct {
	Name string `json:"name"`; Price int `json:"price"`; HP int `json:"hp"`
	Speed float64 `json:"speed"`; Turn float64 `json:"turn"`
	Cannons string `json:"cannons"`; Count int `json:"count"`; Reload int64 `json:"reload"`
	Cargo int `json:"cargo"`; Crew int `json:"crew"`
}
var Ships = map[string]ShipDef{
	"rowboat":  {Name:"Rowboat",Price:0,HP:80,Speed:0.85,Turn:0.04,Cannons:"front",Count:1,Reload:1200,Cargo:100,Crew:1},
	"warship":  {Name:"War Galleon",Price:2000,HP:200,Speed:0.6,Turn:0.025,Cannons:"side",Count:3,Reload:2500,Cargo:200,Crew:5},
	"tradeship":{Name:"Trade Schooner",Price:1500,HP:130,Speed:0.75,Turn:0.032,Cannons:"front",Count:1,Reload:1000,Cargo:1000,Crew:3},
}

type GoodDef struct{ Name,Icon string; Size int }
var Goods = map[string]GoodDef{"coffee":{"Coffee","☕",15},"spice":{"Spice","🌶",20},"beer":{"Beer","🍺",30}}

type OreDef struct{ Name,Icon string; Value int }
var Ores = map[string]OreDef{"iron":{"Iron","⛏",50},"gold":{"Gold","🥇",150},"bronze":{"Bronze","🔶",80}}

type Island struct {
	X,Z,R float64; Name string; Goods map[string]*IGood `json:"goods,omitempty"`
	Ore string `json:"ore,omitempty"`; Safe bool `json:"safe,omitempty"`
}
type IGood struct{ Price,Stock int }
type Rock struct{ X,Z,R float64 }

// Housing
type HouseDef struct{ Name string; Price int; Tier int; StorageCap int }
var HouseDefs = map[string]HouseDef{
	"shack": {"Shack",500,1,200},
	"house": {"House",2000,2,500},
	"villa": {"Villa",5000,3,1000},
}
type HouseSlot struct {
	X,Z float64; Owner string; HouseType string
	Storage map[string]int; StorageUsed int
}

// Market listing
type MarketListing struct {
	ID int `json:"id"`; Seller,SellerName string; Item string; Qty,Price int
}

var tradeIslands = []*Island{
	{X:-450,Z:-450,R:80,Name:"Skull Isle",Goods:map[string]*IGood{"coffee":{40,100},"beer":{25,80}}},
	{X:450,Z:-450,R:90,Name:"Palm Haven",Goods:map[string]*IGood{"spice":{55,60},"coffee":{50,70}}},
	{X:450,Z:450,R:75,Name:"Fort Rock",Goods:map[string]*IGood{"beer":{30,90},"spice":{45,50}}},
	{X:-450,Z:450,R:85,Name:"Treasure Cove",Goods:map[string]*IGood{"coffee":{35,80},"spice":{60,40},"beer":{20,100}}},
}
var safeIsland = &Island{X:0,Z:0,R:140,Name:"Haven (Safe Zone)",Safe:true,
	Goods:map[string]*IGood{"coffee":{45,200},"spice":{50,150},"beer":{22,200}}}
var mineIslands = []*Island{
	{X:0,Z:-350,R:22,Name:"Iron Reef",Ore:"iron"},
	{X:-250,Z:0,R:18,Name:"Gold Shoal",Ore:"gold"},
	{X:250,Z:0,R:20,Name:"Bronze Atoll",Ore:"bronze"},
	{X:0,Z:350,R:18,Name:"Copper Cay",Ore:"iron"},
	{X:-200,Z:-250,R:15,Name:"Nugget Isle",Ore:"gold"},
	{X:200,Z:250,R:16,Name:"Tin Rock",Ore:"bronze"},
}
var allIslands []*Island
var rocks []Rock
var houseSlots []*HouseSlot

func initWorld() {
	allIslands = append(allIslands, safeIsland)
	allIslands = append(allIslands, tradeIslands...)
	allIslands = append(allIslands, mineIslands...)
	for _,il := range allIslands {
		n := int(il.R/12)
		for i:=0;i<n;i++ {
			a:=rand.Float64()*math.Pi*2; d:=5+rand.Float64()*(il.R-12)
			rocks = append(rocks, Rock{il.X+math.Cos(a)*d, il.Z+math.Sin(a)*d, 1.5+rand.Float64()*1.5})
		}
	}
	// House slots on safe island (circle of 8 spots)
	for i:=0;i<8;i++ {
		a := float64(i)/8*math.Pi*2
		houseSlots = append(houseSlots, &HouseSlot{
			X: safeIsland.X+math.Cos(a)*80, Z: safeIsland.Z+math.Sin(a)*80,
			Storage: make(map[string]int),
		})
	}
}

func onLand(x,z float64) *Island {
	for _,il := range allIslands { if math.Hypot(x-il.X,z-il.Z)<il.R { return il } }; return nil
}
func nearShore(x,z float64) *Island {
	for _,il := range allIslands { d:=math.Hypot(x-il.X,z-il.Z); if d<il.R+SHORE&&d>il.R-8{return il} }; return nil
}
func hitRock(x,z,r float64) *Rock {
	for i := range rocks { if math.Hypot(x-rocks[i].X,z-rocks[i].Z)<rocks[i].R+r{return &rocks[i]} }; return nil
}
func isMine(il *Island) bool { return il.Ore!="" }
func inSafeZone(x,z float64) bool { return math.Hypot(x-safeIsland.X,z-safeIsland.Z)<safeIsland.R+20 }
func spawnPos() (float64,float64) {
	for { x:=(rand.Float64()-0.5)*MAP*0.5; z:=(rand.Float64()-0.5)*MAP*0.5; if onLand(x,z)==nil{return x,z} }
}

type Input struct{ Fwd,Back,Left,Right,Act,Jump bool }

type Player struct {
	ID,Name string; Conn *websocket.Conn; mu sync.Mutex
	CX,CZ,CY,CR,CVY float64
	BX,BZ,BR,BS float64
	OnBoat,Swim bool; SwimT int64
	Ship string; Owned []string; Gold,HP,MHP,Score,Sails int; Alive bool
	Inp Input; LastF int64; AX,AZ float64; ActP bool; JoinT int64
	Cargo map[string]int; CargoUsed int
	Inv map[string]int // ores in hand
	Mining bool; MineT int64
	// Crew system
	BoardedOn string // ship owner ID we're riding on ("" = own ship or on foot)
	Sinking float64 // 0=normal, >0 = sinking progress (0-1)
}

func (p *Player) Send(msg interface{}) {
	p.mu.Lock(); defer p.mu.Unlock()
	p.Conn.SetWriteDeadline(time.Now().Add(100*time.Millisecond))
	p.Conn.WriteJSON(msg)
}

type CB struct{ X,Z,Y,VX,VZ,VY float64; Owner string; Born int64 }
type GItem struct{ ID int; X,Z float64; Type string }

type Game struct {
	mu sync.RWMutex; players map[string]*Player; cbs []*CB; items []GItem; itemID int
	market []MarketListing; marketID int
}
func NewGame() *Game { return &Game{players:make(map[string]*Player)} }
func now() int64 { return time.Now().UnixMilli() }

// State types
type PState struct {
	CX,CZ,CY,CR float64; BX,BZ,BR,BS float64
	SL int; OB,SW bool; HP,MHP,SC,G int
	N,SH string; AX,AZ float64
	Cargo map[string]int; CU int; Inv map[string]int; MN bool
	BO string; SK float64 // boardedOn, sinking
}

func (g *Game) buildState() map[string]interface{} {
	p := make(map[string]*PState)
	for id,pl := range g.players {
		p[id] = &PState{
			CX:pl.CX,CZ:pl.CZ,CY:pl.CY,CR:pl.CR,BX:pl.BX,BZ:pl.BZ,BR:pl.BR,BS:pl.BS,
			SL:pl.Sails,OB:pl.OnBoat,SW:pl.Swim,HP:pl.HP,MHP:pl.MHP,SC:pl.Score,G:pl.Gold,
			N:pl.Name,SH:pl.Ship,AX:pl.AX,AZ:pl.AZ,
			Cargo:pl.Cargo,CU:pl.CargoUsed,Inv:pl.Inv,MN:pl.Mining,
			BO:pl.BoardedOn,SK:pl.Sinking,
		}
	}
	cb := make([]map[string]float64, len(g.cbs))
	for i,c := range g.cbs { cb[i]=map[string]float64{"x":c.X,"z":c.Z,"y":c.Y} }
	gi := make([]map[string]interface{}, len(g.items))
	for i,it := range g.items { gi[i]=map[string]interface{}{"id":it.ID,"x":it.X,"z":it.Z,"type":it.Type} }
	// House slots
	hs := make([]map[string]interface{}, len(houseSlots))
	for i,h := range houseSlots {
		hs[i] = map[string]interface{}{"x":h.X,"z":h.Z,"owner":h.Owner,"type":h.HouseType,"su":h.StorageUsed}
	}
	return map[string]interface{}{"t":now(),"p":p,"cb":cb,"gi":gi,"hs":hs}
}

func (g *Game) respawn(p *Player) {
	x,z := spawnPos(); p.BX,p.BZ=x,z; p.BR=rand.Float64()*math.Pi*2; p.BS=0
	p.CX,p.CZ,p.CY,p.CR=x,z,0,p.BR; p.OnBoat,p.Swim,p.Mining=true,false,false
	sh:=Ships[p.Ship]; p.HP,p.MHP=sh.HP,sh.HP; p.Sails=1; p.Alive=true; p.Sinking=0; p.BoardedOn=""
}

func (g *Game) crewCount(ownerID string) int {
	c := 0
	for _,p := range g.players { if p.BoardedOn==ownerID{c++} }
	return c
}

func (g *Game) fire(p *Player) {
	if inSafeZone(p.BX,p.BZ) { return } // no firing in safe zone
	sh:=Ships[p.Ship]; dx,dz:=p.AX-p.BX,p.AZ-p.BZ
	dist:=math.Max(20,math.Min(180,math.Hypot(dx,dz)))
	if sh.Cannons=="front" {
		g.mkCB(p,p.BX+math.Sin(p.BR)*6,p.BZ+math.Cos(p.BR)*6,p.AX,p.AZ,dist)
	} else {
		aim:=math.Atan2(dx,dz); rel:=aim-p.BR
		for rel>math.Pi{rel-=math.Pi*2}; for rel< -math.Pi{rel+=math.Pi*2}
		side:=1.0; if rel<=0{side=-1.0}; sa:=p.BR+side*math.Pi/2
		for i:=0;i<sh.Count;i++ {
			off:=float64(i-1)*4
			ox:=p.BX+math.Sin(p.BR)*off+math.Sin(sa)*5; oz:=p.BZ+math.Cos(p.BR)*off+math.Cos(sa)*5
			g.mkCB(p,ox,oz,p.AX+math.Sin(p.BR)*float64(i-1)*7,p.AZ+math.Cos(p.BR)*float64(i-1)*7,dist)
		}
	}
}
func (g *Game) mkCB(p *Player,sx,sz,tx,tz,dist float64) {
	d:=math.Max(1,math.Hypot(tx-sx,tz-sz)); spd:=2.8; fl:=dist/spd/20
	g.cbs = append(g.cbs, &CB{sx,sz,3.5,(tx-sx)/d*spd,(tz-sz)/d*spd,fl*0.45,p.ID,now()})
}

func (g *Game) tick() {
	g.mu.Lock(); defer g.mu.Unlock()
	n := now()

	for _,p := range g.players {
		if !p.Alive { continue }
		// If sinking, just sink and die
		if p.Sinking > 0 {
			p.Sinking += 0.005
			p.BZ += math.Cos(p.BR)*p.BS*0.3 // drift
			if p.Sinking >= 1 { g.respawn(p) }
			continue
		}
		inp:=p.Inp; sh:=Ships[p.Ship]

		// If boarded on someone else's ship, move on their deck
		if p.BoardedOn != "" {
			owner,ok := g.players[p.BoardedOn]
			if !ok || !owner.Alive || owner.Sinking>0 { p.BoardedOn=""; p.OnBoat=true; continue }
			// Walk on deck relative to ship
			if inp.Left{p.CR+=0.06}; if inp.Right{p.CR-=0.06}
			var mx,mz float64
			if inp.Fwd{mx=math.Sin(p.CR)*WALK*0.5;mz=math.Cos(p.CR)*WALK*0.5}
			// Stay within ship bounds (approx 8 units from center)
			nx,nz := p.CX+mx-owner.BX, p.CZ+mz-owner.BZ
			if math.Hypot(nx,nz) < 8 { p.CX+=mx; p.CZ+=mz } 
			// Disembark
			if inp.Act&&!p.ActP { p.ActP=true
				if shore:=nearShore(owner.BX,owner.BZ); shore!=nil {
					a:=math.Atan2(owner.BX-shore.X,owner.BZ-shore.Z)
					p.CX=shore.X+math.Sin(a)*(shore.R-4); p.CZ=shore.Z+math.Cos(a)*(shore.R-4)
					p.CY=3; p.CR=a; p.BoardedOn=""; p.OnBoat=false; p.Swim=false
				}
			}
			if !inp.Act{p.ActP=false}
			continue
		}

		if p.OnBoat {
			if inp.Left{p.BR+=sh.Turn}; if inp.Right{p.BR-=sh.Turn}
			mx:=[]float64{0,sh.Speed*0.45,sh.Speed}[p.Sails]
			if inp.Fwd{p.BS=math.Min(p.BS+0.04,mx)} else if inp.Back{p.BS=math.Max(p.BS-0.04,-mx*0.2)} else {
				if p.Sails==0{p.BS*=0.95} else {p.BS+=(mx*0.7-p.BS)*0.008}}
			p.BX+=math.Sin(p.BR)*p.BS; p.BZ+=math.Cos(p.BR)*p.BS
			h:=float64(MAP)/2; p.BX=math.Max(-h,math.Min(h,p.BX)); p.BZ=math.Max(-h,math.Min(h,p.BZ))
			for _,il := range allIslands {
				d:=math.Hypot(p.BX-il.X,p.BZ-il.Z); m:=il.R+6
				if d<m{a:=math.Atan2(p.BX-il.X,p.BZ-il.Z);p.BX=il.X+math.Sin(a)*m;p.BZ=il.Z+math.Cos(a)*m;p.BS*=0.2}
			}
			p.CX,p.CZ,p.CR=p.BX,p.BZ,p.BR
			// Move passengers
			for _,op := range g.players {
				if op.BoardedOn==p.ID { op.CX+=math.Sin(p.BR)*p.BS; op.CZ+=math.Cos(p.BR)*p.BS }
			}

			if inp.Act&&!p.ActP { p.ActP=true
				shore:=nearShore(p.BX,p.BZ)
				if shore!=nil {
					a:=math.Atan2(p.BX-shore.X,p.BZ-shore.Z)
					p.CX=shore.X+math.Sin(a)*(shore.R-4); p.CZ=shore.Z+math.Cos(a)*(shore.R-4)
					p.CY=3; p.CR=a; p.OnBoat=false; p.Swim=false; p.Mining=false
				}
			}
			if !inp.Act{p.ActP=false}
		} else {
			if inp.Left{p.CR+=0.06}; if inp.Right{p.CR-=0.06}
			var mx,mz float64
			if inp.Fwd{mx=math.Sin(p.CR)*WALK;mz=math.Cos(p.CR)*WALK}
			if inp.Back{mx=-math.Sin(p.CR)*WALK*0.5;mz=-math.Cos(p.CR)*WALK*0.5}
			nx,nz:=p.CX+mx,p.CZ+mz
			if ob:=hitRock(nx,nz,1);ob==nil{p.CX,p.CZ=nx,nz} else {
				a:=math.Atan2(nx-ob.X,nz-ob.Z);p.CX=ob.X+math.Sin(a)*(ob.R+1.2);p.CZ=ob.Z+math.Cos(a)*(ob.R+1.2)}
			if inp.Jump&&p.CY<=3.05&&!p.Swim{p.CVY=JUMP}
			p.CVY-=GRAV; p.CY+=p.CVY
			land:=onLand(p.CX,p.CZ); gy:=-0.5; if land!=nil{gy=3}
			if p.CY<gy{p.CY=gy;p.CVY=0}
			if land==nil {
				if !p.Swim{p.Swim=true;p.SwimT=n}
				if !inSafeZone(p.CX,p.CZ) && n-p.SwimT>DROWN {
					p.HP-=60; if p.HP<=0{g.respawn(p); p.Send(map[string]interface{}{"t":"sunk","by":"the sea"})} else {
						p.OnBoat=true;p.Swim=false;p.CX,p.CZ,p.CY=p.BX,p.BZ,0}
				}
			} else {
				p.Swim=false
				if p.Mining&&isMine(land)&&n-p.MineT>2000 {
					p.Mining=false; a:=rand.Float64()*math.Pi*2; g.itemID++
					g.items=append(g.items,GItem{g.itemID,p.CX+math.Cos(a)*3,p.CZ+math.Sin(a)*3,land.Ore})
				}
			}
			// Auto-pickup
			for i:=len(g.items)-1;i>=0;i-- {
				gi:=g.items[i]; if math.Hypot(p.CX-gi.X,p.CZ-gi.Z)<3 {
					p.Inv[gi.Type]++; g.items=append(g.items[:i],g.items[i+1:]...)
				}
			}
			// Embark own ship or board others
			if inp.Act&&!p.ActP { p.ActP=true
				// Own ship?
				if math.Hypot(p.CX-p.BX,p.CZ-p.BZ)<EMBARK {
					p.OnBoat=true;p.Swim=false;p.CX,p.CZ,p.CY=p.BX,p.BZ,0;p.Mining=false
				} else {
					// Check other ships nearby
					for oid,op := range g.players {
						if oid==p.ID||!op.Alive||op.Sinking>0 { continue }
						if math.Hypot(p.CX-op.BX,p.CZ-op.BZ)<EMBARK {
							maxCrew := Ships[op.Ship].Crew
							if g.crewCount(oid)<maxCrew-1 { // -1 for owner
								p.BoardedOn=oid; p.OnBoat=false; p.Swim=false
								p.CX,p.CZ=op.BX,op.BZ; p.CY=0
								break
							}
						}
					}
				}
			}
			if !inp.Act{p.ActP=false}
		}
	}

	// Cannonballs
	for i:=len(g.cbs)-1;i>=0;i-- {
		c:=g.cbs[i]; c.X+=c.VX;c.Z+=c.VZ;c.Y+=c.VY;c.VY-=0.045
		if c.Y<0||n-c.Born>CB_LIFE{g.cbs=append(g.cbs[:i],g.cbs[i+1:]...);continue}
		for id,p := range g.players {
			if id==c.Owner||!p.Alive||p.Sinking>0{continue}
			if inSafeZone(p.BX,p.BZ){continue} // safe zone protection
			if math.Hypot(c.X-p.BX,c.Z-p.BZ)<10 {
				p.HP-=CB_DMG; g.cbs=append(g.cbs[:i],g.cbs[i+1:]...)
				// Ship damage effects
				if p.HP<=0 {
					p.Sinking=0.01 // start sinking
					k:=g.players[c.Owner]; if k!=nil{k.Score++;k.Gold+=200}
					p.Send(map[string]interface{}{"t":"sunk","by":func()string{if k!=nil{return k.Name};return"?"}()})
				}
				break
			}
		}
	}

	// Broadcast
	state := g.buildState()
	data,_ := json.Marshal(map[string]interface{}{"t":"s","d":state})
	for _,p := range g.players {
		p.mu.Lock()
		p.Conn.SetWriteDeadline(time.Now().Add(50*time.Millisecond))
		p.Conn.WriteMessage(websocket.TextMessage, data)
		p.mu.Unlock()
	}
}

var upgrader = websocket.Upgrader{CheckOrigin:func(r*http.Request)bool{return true}}

func (g *Game) handleWS(w http.ResponseWriter, r *http.Request) {
	conn,err := upgrader.Upgrade(w,r,nil); if err!=nil{return}; defer conn.Close()
	id:=fmt.Sprintf("p_%d_%d",now(),rand.Intn(9999)); sx,sz:=spawnPos(); sr:=rand.Float64()*math.Pi*2
	p:=&Player{ID:id,Name:"Pirate",Conn:conn,CX:sx,CZ:sz,BX:sx,BZ:sz,BR:sr,AX:sx,AZ:sz+30,
		OnBoat:true,Ship:"rowboat",Owned:[]string{"rowboat"},Gold:GOLD0,HP:80,MHP:80,Sails:1,Alive:true,JoinT:now(),
		Cargo:make(map[string]int),Inv:make(map[string]int)}

	g.mu.Lock(); g.players[id]=p; state:=g.buildState(); g.mu.Unlock()

	// Market listings
	g.mu.RLock()
	ml := make([]map[string]interface{},len(g.market))
	for i,m := range g.market { ml[i]=map[string]interface{}{"id":m.ID,"seller":m.SellerName,"item":m.Item,"qty":m.Qty,"price":m.Price} }
	g.mu.RUnlock()

	p.Send(map[string]interface{}{
		"t":"init","id":id,"islands":tradeIslands,"mineIslands":mineIslands,"safeIsland":safeIsland,
		"rocks":rocks,"map":MAP,"ships":Ships,"goods":Goods,"ores":Ores,"houseDefs":HouseDefs,
		"state":state,"spawnX":sx,"spawnZ":sz,"spawnR":sr,"market":ml,
		"houseSlots":func()[]map[string]interface{}{
			r:=make([]map[string]interface{},len(houseSlots))
			for i,h:=range houseSlots{r[i]=map[string]interface{}{"x":h.X,"z":h.Z,"owner":h.Owner,"type":h.HouseType}}
			return r
		}(),
	})
	log.Printf("+ %s (%d)",id,len(g.players))

	for {
		_,data,err:=conn.ReadMessage(); if err!=nil{break}
		var msg map[string]json.RawMessage; if json.Unmarshal(data,&msg)!=nil{continue}
		var t string; json.Unmarshal(msg["t"],&t)

		g.mu.Lock()
		switch t {
		case "input":
			var im struct{Fwd,Back,Left,Right,Act,Jump bool}; json.Unmarshal(data,&im)
			p.Inp=Input{im.Fwd,im.Back,im.Left,im.Right,im.Act,im.Jump}
		case "aim":
			var am struct{X,Z float64}; json.Unmarshal(data,&am); p.AX,p.AZ=am.X,am.Z
		case "fire":
			if p.OnBoat&&p.Alive&&p.Sinking==0 { sh:=Ships[p.Ship]; if now()-p.LastF>=int64(sh.Reload){p.LastF=now();g.fire(p)} }
		case "setSails":
			var v struct{V int`json:"v"`}; json.Unmarshal(data,&v); if v.V>=0&&v.V<=2{p.Sails=v.V}
		case "setName":
			var v struct{V string`json:"v"`}; json.Unmarshal(data,&v); if len(v.V)>0&&len(v.V)<=16{p.Name=v.V}
		case "buy":
			var v struct{V string`json:"v"`}; json.Unmarshal(data,&v)
			if sh,ok:=Ships[v.V];ok{
				owned:=false; for _,o:=range p.Owned{if o==v.V{owned=true;break}}
				if owned{p.Send(map[string]interface{}{"t":"msg","v":"Already owned!"})} else if p.Gold<sh.Price{
					p.Send(map[string]interface{}{"t":"msg","v":"Not enough gold!"})} else {
					p.Gold-=sh.Price;p.Owned=append(p.Owned,v.V)
					p.Send(map[string]interface{}{"t":"msg","v":"Bought "+sh.Name+"!"})
					p.Send(map[string]interface{}{"t":"upd","gold":p.Gold,"owned":p.Owned})}}
		case "equip":
			var v struct{V string`json:"v"`}; json.Unmarshal(data,&v)
			owned:=false; for _,o:=range p.Owned{if o==v.V{owned=true;break}}
			if owned{p.Ship=v.V;sh:=Ships[v.V];p.MHP,p.HP=sh.HP,sh.HP;p.Cargo=make(map[string]int);p.CargoUsed=0
				p.Send(map[string]interface{}{"t":"upd","ship":v.V,"hp":p.HP,"mhp":p.MHP})}
		case "tp":
			var tm struct{X,Z float64}; json.Unmarshal(data,&tm)
			p.BX,p.BZ,p.CX,p.CZ,p.BS=tm.X,tm.Z,tm.X,tm.Z,0; if !p.OnBoat{p.OnBoat,p.Swim=true,false}
		case "deposit": // deposit ores from hand to ship cargo
			if !p.OnBoat && math.Hypot(p.CX-p.BX,p.CZ-p.BZ)<EMBARK {
				for ore,qty := range p.Inv {
					if qty<=0{continue}
					if od,ok:=Ores[ore];ok{_=od; p.Cargo[ore]+=qty; p.CargoUsed+=qty*10; delete(p.Inv,ore)}
				}
				p.Send(map[string]interface{}{"t":"msg","v":"Deposited ores to ship!"})
			}
		case "buyGood":
			var bm struct{Good string;Qty,Idx int}; json.Unmarshal(data,&bm)
			if bm.Idx>=-1&&!p.OnBoat { // -1 = safe island
				var isl *Island
				if bm.Idx==-1{isl=safeIsland} else if bm.Idx<len(tradeIslands){isl=tradeIslands[bm.Idx]}
				if isl!=nil { if ig,ok:=isl.Goods[bm.Good];ok { if gd,ok:=Goods[bm.Good];ok {
					cap:=Ships[p.Ship].Cargo; if cap==0{p.Send(map[string]interface{}{"t":"msg","v":"No cargo!"});break}
					max:=bm.Qty; if ig.Stock<max{max=ig.Stock}
					if avail:=(cap-p.CargoUsed)/gd.Size;avail<max{max=avail}
					if max<=0{break}; cost:=max*ig.Price; if p.Gold<cost{break}
					p.Gold-=cost;ig.Stock-=max;p.Cargo[bm.Good]+=max;p.CargoUsed+=max*gd.Size
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Bought %d %s",max,gd.Name)})
				}}}
			}
		case "sellGood":
			var sm struct{Good string;Qty,Idx int}; json.Unmarshal(data,&sm)
			if !p.OnBoat {
				var isl *Island
				if sm.Idx==-1{isl=safeIsland} else if sm.Idx>=0&&sm.Idx<len(tradeIslands){isl=tradeIslands[sm.Idx]}
				if isl!=nil { if ig,ok:=isl.Goods[sm.Good];ok {
					have:=p.Cargo[sm.Good]; sell:=sm.Qty; if have<sell{sell=have}; if sell<=0{break}
					price:=int(float64(ig.Price)*1.3); p.Gold+=sell*price
					p.Cargo[sm.Good]-=sell; if p.Cargo[sm.Good]<=0{delete(p.Cargo,sm.Good)}
					p.CargoUsed-=sell*Goods[sm.Good].Size
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Sold %d for %dg",sell,sell*price)})
				}}
			}
		case "sellOre":
			var sm struct{Ore string;Qty int}; json.Unmarshal(data,&sm)
			if od,ok:=Ores[sm.Ore];ok&&!p.OnBoat {
				// Check ship cargo for ores too
				have:=p.Inv[sm.Ore]+p.Cargo[sm.Ore]; sell:=sm.Qty; if have<sell{sell=have}
				if sell>0 {
					// Sell from inv first, then cargo
					fromInv:=p.Inv[sm.Ore]; if fromInv>sell{fromInv=sell}
					fromCargo:=sell-fromInv
					p.Inv[sm.Ore]-=fromInv; if p.Inv[sm.Ore]<=0{delete(p.Inv,sm.Ore)}
					p.Cargo[sm.Ore]-=fromCargo; if p.Cargo[sm.Ore]<=0{delete(p.Cargo,sm.Ore)}
					p.CargoUsed-=fromCargo*10; p.Gold+=sell*od.Value
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Sold %d %s for %dg",sell,od.Name,sell*od.Value)})
				}
			}
		case "mine":
			if !p.OnBoat&&!p.Mining { land:=onLand(p.CX,p.CZ); if land!=nil&&isMine(land){p.Mining=true;p.MineT=now()} }
		case "buyHouse":
			var hm struct{Slot int;Type string}; json.Unmarshal(data,&hm)
			if hm.Slot>=0&&hm.Slot<len(houseSlots) {
				hs:=houseSlots[hm.Slot]; hd,ok:=HouseDefs[hm.Type]
				if ok&&hs.Owner=="" {
					if p.Gold>=hd.Price { p.Gold-=hd.Price;hs.Owner=p.ID;hs.HouseType=hm.Type;hs.Storage=make(map[string]int)
						p.Send(map[string]interface{}{"t":"msg","v":"Bought "+hd.Name+"!"})
						p.Send(map[string]interface{}{"t":"upd","gold":p.Gold})
					} else { p.Send(map[string]interface{}{"t":"msg","v":"Not enough gold!"}) }
				} else if hs.Owner==p.ID && ok && hm.Type!=hs.HouseType {
					// Upgrade
					if p.Gold>=hd.Price{p.Gold-=hd.Price;hs.HouseType=hm.Type
						p.Send(map[string]interface{}{"t":"msg","v":"Upgraded to "+hd.Name+"!"})
						p.Send(map[string]interface{}{"t":"upd","gold":p.Gold})}
				}
			}
		case "houseDeposit": // deposit from cargo/inv to house storage
			var dm struct{Slot int}; json.Unmarshal(data,&dm)
			if dm.Slot>=0&&dm.Slot<len(houseSlots) {
				hs:=houseSlots[dm.Slot]; if hs.Owner==p.ID {
					hd:=HouseDefs[hs.HouseType]; cap:=hd.StorageCap
					// Move all cargo
					for item,qty := range p.Cargo {
						hs.Storage[item]+=qty; hs.StorageUsed+=qty
						delete(p.Cargo,item)
					}
					p.CargoUsed=0
					// Move all inv ores
					for item,qty := range p.Inv {
						hs.Storage[item]+=qty; hs.StorageUsed+=qty
						delete(p.Inv,item)
					}
					if hs.StorageUsed>cap{hs.StorageUsed=cap} // cap it
					p.Send(map[string]interface{}{"t":"msg","v":"Deposited to storage!"})
					p.Send(map[string]interface{}{"t":"houseStorage","slot":dm.Slot,"storage":hs.Storage,"used":hs.StorageUsed,"cap":cap})
				}
			}
		case "houseWithdraw":
			var wm struct{Slot int;Item string;Qty int}; json.Unmarshal(data,&wm)
			if wm.Slot>=0&&wm.Slot<len(houseSlots) {
				hs:=houseSlots[wm.Slot]; if hs.Owner==p.ID {
					have:=hs.Storage[wm.Item]; take:=wm.Qty; if have<take{take=have}
					if take>0 {
						hs.Storage[wm.Item]-=take; if hs.Storage[wm.Item]<=0{delete(hs.Storage,wm.Item)}
						hs.StorageUsed-=take
						// Put in cargo
						p.Cargo[wm.Item]+=take; p.CargoUsed+=take*10
						p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Withdrew %d %s",take,wm.Item)})
					}
				}
			}
		case "marketList": // list item for sale
			var lm struct{Item string;Qty,Price int}; json.Unmarshal(data,&lm)
			have:=p.Cargo[lm.Item]+p.Inv[lm.Item]; if have<lm.Qty{break}
			// Remove from cargo first
			fromCargo:=p.Cargo[lm.Item]; if fromCargo>lm.Qty{fromCargo=lm.Qty}
			fromInv:=lm.Qty-fromCargo
			p.Cargo[lm.Item]-=fromCargo; if p.Cargo[lm.Item]<=0{delete(p.Cargo,lm.Item)}
			p.Inv[lm.Item]-=fromInv; if p.Inv[lm.Item]<=0{delete(p.Inv,lm.Item)}
			p.CargoUsed-=fromCargo*10
			g.marketID++
			g.market=append(g.market,MarketListing{g.marketID,p.ID,p.Name,lm.Item,lm.Qty,lm.Price})
			p.Send(map[string]interface{}{"t":"msg","v":"Listed on market!"})
		case "marketBuy":
			var bm struct{ID int}; json.Unmarshal(data,&bm)
			for i,m := range g.market {
				if m.ID==bm.ID {
					cost:=m.Qty*m.Price; if p.Gold<cost{p.Send(map[string]interface{}{"t":"msg","v":"Not enough gold!"});break}
					p.Gold-=cost; p.Cargo[m.Item]+=m.Qty; p.CargoUsed+=m.Qty*10
					if seller,ok:=g.players[m.Seller];ok{seller.Gold+=cost}
					g.market=append(g.market[:i],g.market[i+1:]...)
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Bought %d %s!",m.Qty,m.Item)})
					break
				}
			}
		case "getMarket":
			ml:=make([]map[string]interface{},len(g.market))
			for i,m:=range g.market{ml[i]=map[string]interface{}{"id":m.ID,"seller":m.SellerName,"item":m.Item,"qty":m.Qty,"price":m.Price}}
			p.Send(map[string]interface{}{"t":"market","listings":ml})
		case "getHouseStorage":
			var hm struct{Slot int}; json.Unmarshal(data,&hm)
			if hm.Slot>=0&&hm.Slot<len(houseSlots){
				hs:=houseSlots[hm.Slot]; if hs.Owner==p.ID{
					hd:=HouseDefs[hs.HouseType]
					p.Send(map[string]interface{}{"t":"houseStorage","slot":hm.Slot,"storage":hs.Storage,"used":hs.StorageUsed,"cap":hd.StorageCap})
				}}
		}
		g.mu.Unlock()
	}
	g.mu.Lock(); delete(g.players,id); g.mu.Unlock()
	g.mu.RLock()
	d,_:=json.Marshal(map[string]interface{}{"t":"left","id":id})
	for _,op:=range g.players{op.mu.Lock();op.Conn.WriteMessage(websocket.TextMessage,d);op.mu.Unlock()}
	g.mu.RUnlock()
	log.Printf("- %s",id)
}

func main() {
	rand.Seed(time.Now().UnixNano()); initWorld()
	game:=NewGame()
	go func(){tick:=time.NewTicker(time.Second/TICK);for range tick.C{game.tick()}}()
	http.Handle("/",http.FileServer(http.Dir("public")))
	http.HandleFunc("/ws",game.handleWS)
	http.HandleFunc("/healthz",func(w http.ResponseWriter,r*http.Request){w.Write([]byte("OK"))})
	port:=os.Getenv("PORT"); if port==""{port="3000"}
	log.Printf("Krew3D Go on :%s",port); log.Fatal(http.ListenAndServe(":"+port,nil))
}
