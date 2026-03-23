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
	TICK=20; MAP=6000; WALK=0.7; JUMP=0.45; GRAV=0.035
	DROWN=4000; EMBARK=20; SHORE=16; CB_LIFE=6000; CB_DMG=18; GOLD0=5000
)

// ── SHIPS ──
type ShipDef struct {
	Name string `json:"name"`; Price int `json:"price"`; HP int `json:"hp"`
	Speed float64 `json:"speed"`; Turn float64 `json:"turn"`
	Cannons string `json:"cannons"`; Count int `json:"count"`; Reload int64 `json:"reload"`
	Cargo int `json:"cargo"`; Crew int `json:"crew"`; DeckLen float64 `json:"deckLen"`
	RepairCostPerHP int `json:"repairCost"`
}
var Ships = map[string]ShipDef{
	"rowboat":  {"Rowboat",0,80,0.85,0.04,"front",1,1200,100,1,4,1},
	"warship":  {"War Galleon",2000,200,0.6,0.025,"side",3,2500,200,5,9,3},
	"tradeship":{"Trade Schooner",1500,130,0.75,0.032,"front",1,1000,1000,3,7,2},
}

// ── GOODS (legal trade) ──
type GoodDef struct{ Name string `json:"name"`; Icon string `json:"icon"`; Size int `json:"size"` }
var Goods = map[string]GoodDef{
	"coffee":{"Coffee","☕",15},"spice":{"Spice","🌶",20},"beer":{"Beer","🍺",30},
	"silk":{"Silk","🧶",10},"timber":{"Timber","🪵",40},"cloth":{"Cloth","🧵",12},
}

// ── ORES & CRAFTING ──
type OreDef struct{ Name string `json:"name"`; Icon string `json:"icon"`; Value int `json:"value"` }
var Ores = map[string]OreDef{"iron":{"Iron","⛏",50},"gold":{"Gold","🥇",150},"bronze":{"Bronze","🔶",80}}

type CraftRecipe struct {
	Name string `json:"name"`; Result string `json:"result"`; Icon string `json:"icon"`
	Needs map[string]int `json:"needs"`; SellPrice int `json:"sellPrice"`
}
var Recipes = map[string]CraftRecipe{
	"iron_bar":   {"Iron Bar","iron_bar","🔧",map[string]int{"iron":3},120},
	"gold_bar":   {"Gold Bar","gold_bar","🥇",map[string]int{"gold":2},250},
	"bronze_bar": {"Bronze Bar","bronze_bar","🔶",map[string]int{"bronze":3},180},
	"cannon_upgrade":{"Cannon Upgrade","cannon_upgrade","💥",map[string]int{"iron_bar":2,"bronze_bar":1},500},
	"sail_upgrade":  {"Sail Upgrade","sail_upgrade","⛵",map[string]int{"cloth":5,"timber":2},400},
	"hull_upgrade":  {"Hull Upgrade","hull_upgrade","🛡",map[string]int{"iron_bar":3,"timber":3},600},
}

// ── ILLEGAL GOODS ──
type IllegalDef struct{ Name string `json:"name"`; Icon string `json:"icon"`; Size int `json:"size"`; BuyPrice int `json:"buyPrice"`; SellPrice int `json:"sellPrice"` }
var Illegals = map[string]IllegalDef{
	"rum":       {"Smuggled Rum","🍶",20,30,120},
	"gunpowder": {"Gunpowder","💣",25,50,180},
	"stolen_gold":{"Stolen Gold","💰",15,80,250},
}

// ── ISLANDS ──
type Island struct {
	X float64 `json:"x"`; Z float64 `json:"z"`; R float64 `json:"r"`
	Name string `json:"name"`; Goods map[string]*IGood `json:"goods,omitempty"`
	Ore string `json:"ore,omitempty"`; Safe bool `json:"safe,omitempty"`
	Hidden bool `json:"hidden,omitempty"` // smuggler port
}
type IGood struct{ Price int `json:"price"`; Stock int `json:"stock"` }
type Rock struct{ X float64 `json:"x"`; Z float64 `json:"z"`; R float64 `json:"r"` }

type HouseDef struct{ Name string `json:"name"`; Price int `json:"price"`; Tier int `json:"tier"`; Cap int `json:"cap"` }
var HouseDefs = map[string]HouseDef{
	"shack":{"Shack",500,1,200},"house":{"House",2000,2,500},"villa":{"Villa",5000,3,1000},
}
type HouseSlot struct {
	X float64 `json:"x"`; Z float64 `json:"z"`; Owner string `json:"owner"`
	Type string `json:"type"`; Storage map[string]int `json:"storage"`; Used int `json:"used"`
}
type MarketListing struct{ ID int; Seller,SName,Item string; Qty,Price int }

var safeIsland = &Island{X:0,Z:0,R:250,Name:"Haven (Safe)",Safe:true,
	Goods:map[string]*IGood{"coffee":{45,500},"spice":{50,400},"beer":{22,600},"silk":{80,200},"cloth":{30,400},"timber":{25,300}}}
var tradeIslands = []*Island{
	{X:-2000,Z:-2000,R:300,Name:"Skull Isle",Goods:map[string]*IGood{"coffee":{35,200},"beer":{20,150},"timber":{20,200}}},
	{X:2000,Z:-2000,R:350,Name:"Palm Haven",Goods:map[string]*IGood{"spice":{45,120},"silk":{60,100},"cloth":{25,300}}},
	{X:2000,Z:2000,R:280,Name:"Fort Rock",Goods:map[string]*IGood{"beer":{28,180},"spice":{40,100},"timber":{18,250}}},
	{X:-2000,Z:2000,R:320,Name:"Treasure Cove",Goods:map[string]*IGood{"coffee":{30,160},"silk":{70,80},"cloth":{22,350}}},
}
// Hidden smuggler port
var hiddenPort = &Island{X:1500,Z:-800,R:50,Name:"Black Cove",Hidden:true}
var mineIslands = []*Island{
	{X:800,Z:-1200,R:60,Name:"Iron Reef",Ore:"iron"},
	{X:-1000,Z:600,R:50,Name:"Gold Shoal",Ore:"gold"},
	{X:1200,Z:800,R:55,Name:"Bronze Atoll",Ore:"bronze"},
	{X:-600,Z:-1400,R:45,Name:"Copper Cay",Ore:"iron"},
	{X:-1400,Z:-800,R:40,Name:"Nugget Isle",Ore:"gold"},
	{X:1400,Z:1400,R:48,Name:"Tin Rock",Ore:"bronze"},
}
var allIslands []*Island
var rocks []Rock
var houseSlots []*HouseSlot

func findChar(s string,c byte)int{for i:=0;i<len(s);i++{if s[i]==c{return i}};return -1}
func atoi(s string)int{v:=0;for _,c:=range s{if c>='0'&&c<='9'{v=v*10+int(c-'0')}};return v}

func initWorld() {
	allIslands = append(allIslands, safeIsland)
	allIslands = append(allIslands, tradeIslands...)
	allIslands = append(allIslands, hiddenPort)
	allIslands = append(allIslands, mineIslands...)
	for _,il := range allIslands {
		n := int(il.R/15); if n > 20 { n = 20 }
		for i:=0;i<n;i++ {
			a:=rand.Float64()*math.Pi*2; d:=8+rand.Float64()*(il.R-15)
			rocks = append(rocks, Rock{il.X+math.Cos(a)*d, il.Z+math.Sin(a)*d, 2+rand.Float64()*2})
		}
	}
	for i:=0;i<12;i++ {
		a := float64(i)/12*math.Pi*2
		houseSlots = append(houseSlots, &HouseSlot{X:safeIsland.X+math.Cos(a)*150,Z:safeIsland.Z+math.Sin(a)*150,Storage:make(map[string]int)})
	}
}

func onLand(x,z float64)*Island{for _,il:=range allIslands{if math.Hypot(x-il.X,z-il.Z)<il.R{return il}};return nil}
func nearShore(x,z float64)*Island{for _,il:=range allIslands{d:=math.Hypot(x-il.X,z-il.Z);if d<il.R+SHORE&&d>il.R-10{return il}};return nil}
func hitRock(x,z,r float64)*Rock{for i:=range rocks{if math.Hypot(x-rocks[i].X,z-rocks[i].Z)<rocks[i].R+r{return &rocks[i]}};return nil}
func inSafe(x,z float64)bool{return math.Hypot(x-safeIsland.X,z-safeIsland.Z)<safeIsland.R+30}
func spawnPos()(float64,float64){for{a:=rand.Float64()*math.Pi*2;d:=safeIsland.R+40+rand.Float64()*100;x,z:=safeIsland.X+math.Cos(a)*d,safeIsland.Z+math.Sin(a)*d;if onLand(x,z)==nil{return x,z}}}

type Input struct{Fwd,Back,Left,Right,Act,Jump bool}

type Player struct {
	ID,Name string; Conn *websocket.Conn; mu sync.Mutex
	CX,CZ,CY,CR,CVY,DX,DZ float64
	BX,BZ,BR,BS float64
	OnBoat,Swim bool; SwimT int64
	Ship string; Owned []string; Gold,HP,MHP,Score,Sails int; Alive bool
	Inp Input; LastF int64; AX,AZ float64; ActP bool; JoinT int64
	Cargo map[string]int; CargoUsed int; Inv map[string]int
	Mining bool; MineT int64; BoardedOn string; Sinking float64
	Wanted,Kills int; CraftedItems map[string]int
	// XP & Skills
	XP,Level int
	SkillRange int // extra cannon range (0-5)
	SkillTrade int // trade discount % (0-5)
	SkillSpeed int // ship speed bonus (0-5)
	SkillHP    int // extra HP (0-5)
	SkillPts   int // unspent skill points
	EmbarkT    int64 // embark/disembark timer start
}
func (p *Player) Send(msg interface{}) {
	p.mu.Lock(); defer p.mu.Unlock()
	p.Conn.SetWriteDeadline(time.Now().Add(100*time.Millisecond))
	p.Conn.WriteJSON(msg)
}

type CB struct{X,Z,Y,VX,VZ,VY float64;Owner string;Born int64}
type GItem struct{ID int;X,Z float64;Type string}

type Game struct {
	mu sync.RWMutex; players map[string]*Player; cbs []*CB; items []GItem; itemID int
	market []MarketListing; mktID int
}
func NewGame()*Game{return &Game{players:make(map[string]*Player)}}
func now()int64{return time.Now().UnixMilli()}

type PState struct {
	CX float64 `json:"cx"`; CZ float64 `json:"cz"`; CY float64 `json:"cy"`; CR float64 `json:"cr"`
	BX float64 `json:"bx"`; BZ float64 `json:"bz"`; BR float64 `json:"br"`; BS float64 `json:"bs"`
	SL int `json:"sl"`; OB bool `json:"ob"`; SW bool `json:"sw"`
	HP int `json:"hp"`; MHP int `json:"mhp"`; SC int `json:"sc"`; G int `json:"g"`
	N string `json:"n"`; SH string `json:"sh"`; AX float64 `json:"ax"`; AZ float64 `json:"az"`
	Cargo map[string]int `json:"cargo"`; CU int `json:"cu"`; Inv map[string]int `json:"inv"`; MN bool `json:"mn"`
	BO string `json:"bo"`; SK float64 `json:"sk"`; W int `json:"w"`
	CI map[string]int `json:"ci"`
	XP int `json:"xp"`; LV int `json:"lv"`; SP int `json:"sp"` // skill points
	SR int `json:"sr"`; ST int `json:"st"`; SS int `json:"ss"`; SHP int `json:"shp"` // skills
}

func (g *Game) buildState() map[string]interface{} {
	p:=make(map[string]*PState)
	for id,pl:=range g.players{p[id]=&PState{CX:pl.CX,CZ:pl.CZ,CY:pl.CY,CR:pl.CR,BX:pl.BX,BZ:pl.BZ,BR:pl.BR,BS:pl.BS,
		SL:pl.Sails,OB:pl.OnBoat,SW:pl.Swim,HP:pl.HP,MHP:pl.MHP,SC:pl.Score,G:pl.Gold,N:pl.Name,SH:pl.Ship,
		AX:pl.AX,AZ:pl.AZ,Cargo:pl.Cargo,CU:pl.CargoUsed,Inv:pl.Inv,MN:pl.Mining,BO:pl.BoardedOn,SK:pl.Sinking,
		W:pl.Wanted,CI:pl.CraftedItems,XP:pl.XP,LV:pl.Level,SP:pl.SkillPts,
		SR:pl.SkillRange,ST:pl.SkillTrade,SS:pl.SkillSpeed,SHP:pl.SkillHP}}
	cb:=make([]map[string]float64,len(g.cbs))
	for i,c:=range g.cbs{cb[i]=map[string]float64{"x":c.X,"z":c.Z,"y":c.Y}}
	gi:=make([]map[string]interface{},len(g.items))
	for i,it:=range g.items{gi[i]=map[string]interface{}{"id":it.ID,"x":it.X,"z":it.Z,"type":it.Type}}
	hs:=make([]map[string]interface{},len(houseSlots))
	for i,h:=range houseSlots{hs[i]=map[string]interface{}{"x":h.X,"z":h.Z,"owner":h.Owner,"type":h.Type,"su":h.Used}}
	return map[string]interface{}{"t":now(),"p":p,"cb":cb,"gi":gi,"hs":hs}
}

func (g *Game) respawn(p *Player) {
	x,z:=spawnPos();p.BX,p.BZ=x,z;p.BR=rand.Float64()*math.Pi*2;p.BS=0
	p.CX,p.CZ,p.CY,p.CR=x,z,0,p.BR;p.OnBoat,p.Swim,p.Mining=true,false,false;p.BoardedOn=""
	sh:=Ships[p.Ship];p.MHP=sh.HP+p.SkillHP*20;p.HP=p.MHP;p.Sails=0;p.Alive=true;p.Sinking=0
}
func (p *Player) addXP(amt int){p.XP+=amt;need:=100+p.Level*80;for p.XP>=need{p.XP-=need;p.Level++;p.SkillPts++;need=100+p.Level*80}}

func (g *Game) fire(p *Player) {
	if inSafe(p.BX,p.BZ){return}
	sh:=Ships[p.Ship];dx,dz:=p.AX-p.BX,p.AZ-p.BZ
	baseRange:=100.0+float64(p.SkillRange)*25
	dist:=math.Max(15,math.Min(baseRange,math.Hypot(dx,dz)))
	if sh.Cannons=="front"{g.mkCB(p,p.BX+math.Sin(p.BR)*6,p.BZ+math.Cos(p.BR)*6,p.AX,p.AZ,dist)} else {
		aim:=math.Atan2(dx,dz);rel:=aim-p.BR;for rel>math.Pi{rel-=math.Pi*2};for rel< -math.Pi{rel+=math.Pi*2}
		absRel:=math.Abs(rel)
		if absRel<0.44||absRel>2.7{return} // can't fire within 25° of bow or stern
		side:=1.0;if rel<=0{side=-1.0};sa:=p.BR+side*math.Pi/2
		for i:=0;i<sh.Count;i++{off:=float64(i-1)*4
			ox:=p.BX+math.Sin(p.BR)*off+math.Sin(sa)*5;oz:=p.BZ+math.Cos(p.BR)*off+math.Cos(sa)*5
			g.mkCB(p,ox,oz,p.AX+math.Sin(p.BR)*float64(i-1)*7,p.AZ+math.Cos(p.BR)*float64(i-1)*7,dist)}}
}
func (g *Game) mkCB(p *Player,sx,sz,tx,tz,dist float64){
	d:=math.Max(1,math.Hypot(tx-sx,tz-sz));spd:=2.8;fl:=dist/spd/20
	// Add ship velocity to cannonball (physics)
	svx:=math.Sin(p.BR)*p.BS*0.4;svz:=math.Cos(p.BR)*p.BS*0.4
	g.cbs=append(g.cbs,&CB{sx,sz,3.5,(tx-sx)/d*spd+svx,(tz-sz)/d*spd+svz,fl*0.45,p.ID,now()})
}

func (g *Game) tick() {
	g.mu.Lock();defer g.mu.Unlock();n:=now()
	for _,p:=range g.players{
		if !p.Alive{continue}
		if p.Sinking>0{p.Sinking+=0.005;if p.Sinking>=1{g.respawn(p)};continue}
		inp:=p.Inp;sh:=Ships[p.Ship]
		if p.BoardedOn!=""{
			owner:=g.players[p.BoardedOn];if owner==nil||!owner.Alive||owner.Sinking>0{p.BoardedOn="";p.OnBoat=true;continue}
			oSh:=Ships[owner.Ship];if inp.Left{p.CR+=0.05};if inp.Right{p.CR-=0.05}
			var mx,mz float64;if inp.Fwd{mx=math.Sin(p.CR)*WALK*0.4;mz=math.Cos(p.CR)*WALK*0.4}
			p.DX+=mx;p.DZ+=mz;hw:=oSh.DeckLen*0.4;hl:=oSh.DeckLen
			if p.DX< -hw{p.DX=-hw};if p.DX>hw{p.DX=hw};if p.DZ< -hl{p.DZ=-hl};if p.DZ>hl{p.DZ=hl}
			sin,cos:=math.Sin(owner.BR),math.Cos(owner.BR)
			p.CX=owner.BX+sin*p.DZ+cos*p.DX;p.CZ=owner.BZ+cos*p.DZ-sin*p.DX;p.CY=2.5
			if inp.Act&&!p.ActP{p.ActP=true;if shore:=nearShore(owner.BX,owner.BZ);shore!=nil{
				a:=math.Atan2(owner.BX-shore.X,owner.BZ-shore.Z);p.CX=shore.X+math.Sin(a)*(shore.R-6)
				p.CZ=shore.Z+math.Cos(a)*(shore.R-6);p.CY=3;p.CR=a;p.BoardedOn="";p.OnBoat=false;p.Swim=false;p.DX=0;p.DZ=0}}
			if !inp.Act{p.ActP=false};continue
		}
		if p.OnBoat{
			if inp.Left{p.BR+=sh.Turn};if inp.Right{p.BR-=sh.Turn}
			mx:=[]float64{0,sh.Speed*0.45,sh.Speed}[p.Sails]*(1+float64(p.SkillSpeed)*0.06)
			if inp.Fwd{p.BS=math.Min(p.BS+0.04,mx)}else if inp.Back{p.BS=math.Max(p.BS-0.04,-mx*0.2)}else{
				if p.Sails==0{p.BS*=0.95}else{p.BS+=(mx*0.7-p.BS)*0.008}}
			p.BX+=math.Sin(p.BR)*p.BS;p.BZ+=math.Cos(p.BR)*p.BS
			h:=float64(MAP)/2;p.BX=math.Max(-h,math.Min(h,p.BX));p.BZ=math.Max(-h,math.Min(h,p.BZ))
			for _,il:=range allIslands{d:=math.Hypot(p.BX-il.X,p.BZ-il.Z);m:=il.R+8
				if d<m{a:=math.Atan2(p.BX-il.X,p.BZ-il.Z);p.BX=il.X+math.Sin(a)*m;p.BZ=il.Z+math.Cos(a)*m;p.BS*=0.2}}
			// Ship-to-ship collision
			for oid,op:=range g.players{if oid==p.ID||!op.Alive||op.Sinking>0||!op.OnBoat{continue}
				d:=math.Hypot(p.BX-op.BX,p.BZ-op.BZ);minD:=12.0 // ship radius
				if d<minD&&d>0.1{a:=math.Atan2(p.BX-op.BX,p.BZ-op.BZ);push:=(minD-d)*0.5
					p.BX+=math.Sin(a)*push;p.BZ+=math.Cos(a)*push
					op.BX-=math.Sin(a)*push;op.BZ-=math.Cos(a)*push
					// Transfer momentum
					p.BS*=0.7;op.BS*=0.7}}
			p.CX,p.CZ,p.CR=p.BX,p.BZ,p.BR;p.CY=2.0
			// Boat pickup floating loot
			for i:=len(g.items)-1;i>=0;i--{gi:=g.items[i];if math.Hypot(p.BX-gi.X,p.BZ-gi.Z)<15{
				if idx:=findChar(gi.Type,'_');idx>0{name:=gi.Type[:idx];qty:=atoi(gi.Type[idx+1:])
					if name=="gold"{p.Gold+=qty;p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("+%dg loot!",qty)})} else {p.Cargo[name]+=qty}
				} else {p.Inv[gi.Type]++}
				g.items=append(g.items[:i],g.items[i+1:]...)}}
			// Disembark: E toggles countdown
			if inp.Act&&!p.ActP{p.ActP=true
				if p.EmbarkT>0{p.EmbarkT=0;p.Send(map[string]interface{}{"t":"msg","v":"Cancelled"})} else {
					shore:=nearShore(p.BX,p.BZ);if shore!=nil{p.EmbarkT=n;p.Send(map[string]interface{}{"t":"msg","v":"Disembarking 3..."});p.Send(map[string]interface{}{"t":"cd","v":3})}}}
			if !inp.Act{p.ActP=false}
			if p.EmbarkT>0{elapsed:=n-p.EmbarkT;rem:=3-int(elapsed/1000)
				if rem>0&&int(elapsed/1000)!=int((elapsed-50)/1000){p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Disembarking %d...",rem)});p.Send(map[string]interface{}{"t":"cd","v":rem})}
				if elapsed>=3000{shore:=nearShore(p.BX,p.BZ);if shore!=nil{
					a:=math.Atan2(p.BX-shore.X,p.BZ-shore.Z);p.CX=shore.X+math.Sin(a)*(shore.R-6)
					p.CZ=shore.Z+math.Cos(a)*(shore.R-6);p.CY=3;p.CR=a;p.OnBoat=false;p.Swim=false;p.Mining=false};p.EmbarkT=0}}
		}else{
			// Classic controls: A/D turn, W/S move forward/back
			if inp.Left{p.CR+=0.07};if inp.Right{p.CR-=0.07}
			var mx,mz float64
			if inp.Fwd{mx=math.Sin(p.CR)*WALK;mz=math.Cos(p.CR)*WALK}
			if inp.Back{mx=-math.Sin(p.CR)*WALK*0.5;mz=-math.Cos(p.CR)*WALK*0.5}
			nx,nz:=p.CX+mx,p.CZ+mz
			// Rock collision
			if ob:=hitRock(nx,nz,1);ob!=nil{a:=math.Atan2(nx-ob.X,nz-ob.Z);nx=ob.X+math.Sin(a)*(ob.R+1.5);nz=ob.Z+math.Cos(a)*(ob.R+1.5)}
			// House collision (radius 5)
			for _,hs:=range houseSlots{if hs.Owner!=""&&hs.Type!=""{d:=math.Hypot(nx-hs.X,nz-hs.Z);if d<6{a:=math.Atan2(nx-hs.X,nz-hs.Z);nx=hs.X+math.Sin(a)*6;nz=hs.Z+math.Cos(a)*6}}}
			p.CX,p.CZ=nx,nz
			if inp.Jump&&p.CY<=3.2&&!p.Swim{p.CVY=JUMP};p.CVY-=GRAV;p.CY+=p.CVY
			land:=onLand(p.CX,p.CZ);gy:=0.0;if land!=nil{gy=3.0};if p.CY<gy{p.CY=gy;p.CVY=0}
			// Keep on island edge (don't clip through terrain)
			if land!=nil{d:=math.Hypot(p.CX-land.X,p.CZ-land.Z);if d>land.R-2{a:=math.Atan2(p.CX-land.X,p.CZ-land.Z);p.CX=land.X+math.Sin(a)*(land.R-2);p.CZ=land.Z+math.Cos(a)*(land.R-2)}}
			if land==nil{if !p.Swim{p.Swim=true;p.SwimT=n}
				if !inSafe(p.CX,p.CZ)&&n-p.SwimT>DROWN{p.HP-=60;if p.HP<=0{g.respawn(p);p.Send(map[string]interface{}{"t":"sunk","by":"the sea"})}else{
					p.OnBoat=true;p.Swim=false;p.CX,p.CZ,p.CY=p.BX,p.BZ,0}}}else{p.Swim=false
				if p.Mining&&p.MineT>0&&n-p.MineT>2000{p.Mining=false;a:=rand.Float64()*math.Pi*2;g.itemID++
					g.items=append(g.items,GItem{g.itemID,p.CX+math.Cos(a)*3,p.CZ+math.Sin(a)*3,land.Ore})}}
			for i:=len(g.items)-1;i>=0;i--{gi:=g.items[i];if math.Hypot(p.CX-gi.X,p.CZ-gi.Z)<4.5{
				if idx:=findChar(gi.Type,'_');idx>0{
					name:=gi.Type[:idx];qStr:=gi.Type[idx+1:]
					qty:=atoi(qStr)
					if name=="gold"{p.Gold+=qty;p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("+%dg loot!",qty)})} else {p.Cargo[name]+=qty}
				} else {p.Inv[gi.Type]++}
				g.items=append(g.items[:i],g.items[i+1:]...)}}
			// Embark: E toggles countdown
			if inp.Act&&!p.ActP{p.ActP=true
				if p.EmbarkT>0{p.EmbarkT=0;p.Send(map[string]interface{}{"t":"msg","v":"Cancelled"})} else {
					if math.Hypot(p.CX-p.BX,p.CZ-p.BZ)<EMBARK{p.EmbarkT=n;p.Send(map[string]interface{}{"t":"msg","v":"Boarding 3..."})} else {
						// Instant board other player's ship (no countdown)
						for oid,op:=range g.players{if oid==p.ID||!op.Alive||op.Sinking>0{continue}
							if math.Hypot(p.CX-op.BX,p.CZ-op.BZ)<EMBARK{if g.crewCount(oid)<Ships[op.Ship].Crew-1{
								p.BoardedOn=oid;p.OnBoat=false;p.Swim=false;p.DX=0;p.DZ=0;p.CY=2.5;break}}}}}}
			if !inp.Act{p.ActP=false}
			if p.EmbarkT>0{elapsed:=n-p.EmbarkT;rem:=3-int(elapsed/1000)
				if rem>0&&int(elapsed/1000)!=int((elapsed-50)/1000){p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Boarding %d...",rem)})}
				if elapsed>=3000{p.OnBoat=true;p.Swim=false;p.CX,p.CZ,p.CY=p.BX,p.BZ,0;p.Mining=false;p.Sails=0;p.EmbarkT=0}}
		}
	}
	// Cannonballs
	for i:=len(g.cbs)-1;i>=0;i--{c:=g.cbs[i];c.X+=c.VX;c.Z+=c.VZ;c.Y+=c.VY;c.VY-=0.045
		if c.Y<0||n-c.Born>CB_LIFE{g.cbs=append(g.cbs[:i],g.cbs[i+1:]...);continue}
		for id,p:=range g.players{if id==c.Owner||!p.Alive||p.Sinking>0||inSafe(p.BX,p.BZ){continue}
			if math.Hypot(c.X-p.BX,c.Z-p.BZ)<12{p.HP-=CB_DMG;g.cbs=append(g.cbs[:i],g.cbs[i+1:]...)
				if p.HP<=0{p.Sinking=0.01;k:=g.players[c.Owner]
					if k!=nil{k.Score++;k.Gold+=100+p.Wanted/2;k.Kills++;k.addXP(50)
						// Drop loot barrels
						dropX:=p.BX;dropZ:=p.BZ
						// Drop gold barrel
						if p.Gold>50{dropGold:=p.Gold/4;p.Gold-=dropGold
							g.itemID++;g.items=append(g.items,GItem{g.itemID,dropX+float64(rand.Intn(8)-4),dropZ+float64(rand.Intn(8)-4),fmt.Sprintf("gold_%d",dropGold)})}
						// Drop cargo barrels
						for item,qty:=range p.Cargo{if qty>0{drop:=qty/2;if drop<1{drop=1}
							g.itemID++;g.items=append(g.items,GItem{g.itemID,dropX+float64(rand.Intn(12)-6),dropZ+float64(rand.Intn(12)-6),item+"_"+fmt.Sprintf("%d",drop)})
							p.Cargo[item]-=drop;if p.Cargo[item]<=0{delete(p.Cargo,item)}}}
						p.Send(map[string]interface{}{"t":"sunk","by":k.Name})}
					if p.Wanted>0{p.Wanted=p.Wanted/2} // halve bounty on death
				}else if k:=g.players[c.Owner];k!=nil{k.Wanted+=5} // gain wanted for attacking
				break}}}
	// Broadcast
	state:=g.buildState()
	data,_:=json.Marshal(map[string]interface{}{"t":"s","d":state})
	for _,p:=range g.players{p.mu.Lock();p.Conn.SetWriteDeadline(time.Now().Add(50*time.Millisecond))
		p.Conn.WriteMessage(websocket.TextMessage,data);p.mu.Unlock()}
}

func (g *Game) crewCount(oid string)int{c:=0;for _,p:=range g.players{if p.BoardedOn==oid{c++}};return c}

var upgrader=websocket.Upgrader{CheckOrigin:func(r*http.Request)bool{return true}}

func (g *Game) handleWS(w http.ResponseWriter, r *http.Request) {
	conn,err:=upgrader.Upgrade(w,r,nil);if err!=nil{return};defer conn.Close()
	id:=fmt.Sprintf("p_%d_%d",now(),rand.Intn(9999));sx,sz:=spawnPos();sr:=rand.Float64()*math.Pi*2
	p:=&Player{ID:id,Name:"Pirate",Conn:conn,CX:sx,CZ:sz,BX:sx,BZ:sz,BR:sr,AX:sx,AZ:sz+30,
		OnBoat:true,Ship:"rowboat",Owned:[]string{"rowboat"},Gold:GOLD0,HP:80,MHP:80,Sails:0,Alive:true,JoinT:now(),
		Cargo:make(map[string]int),Inv:make(map[string]int),CraftedItems:make(map[string]int)}

	g.mu.Lock();g.players[id]=p;state:=g.buildState();g.mu.Unlock()

	p.Send(map[string]interface{}{
		"t":"init","id":id,"islands":tradeIslands,"mineIslands":mineIslands,"safeIsland":safeIsland,
		"hiddenPort":hiddenPort,"rocks":rocks,"map":MAP,"ships":Ships,"goods":Goods,"ores":Ores,
		"recipes":Recipes,"illegals":Illegals,"houseDefs":HouseDefs,"houseSlots":houseSlots,
		"state":state,"spawnX":sx,"spawnZ":sz,"spawnR":sr,
	})

	for{
		_,data,err:=conn.ReadMessage();if err!=nil{break}
		var msg map[string]json.RawMessage;if json.Unmarshal(data,&msg)!=nil{continue}
		var t string;json.Unmarshal(msg["t"],&t)
		g.mu.Lock()
		switch t{
		case "input":
			var im struct{Fwd,Back,Left,Right,Act,Jump bool};json.Unmarshal(data,&im)
			p.Inp=Input{im.Fwd,im.Back,im.Left,im.Right,im.Act,im.Jump}
		case "aim":var am struct{X,Z float64};json.Unmarshal(data,&am);p.AX,p.AZ=am.X,am.Z
		case "fire":
			if p.OnBoat&&p.Alive&&p.Sinking==0{sh:=Ships[p.Ship];if now()-p.LastF>=int64(sh.Reload){p.LastF=now();g.fire(p)}}
		case "setSails":var v struct{V int`json:"v"`};json.Unmarshal(data,&v);if v.V>=0&&v.V<=2{p.Sails=v.V}
		case "setName":var v struct{V string`json:"v"`};json.Unmarshal(data,&v);if len(v.V)>0&&len(v.V)<=16{p.Name=v.V}
		case "buy":
			var v struct{V string`json:"v"`};json.Unmarshal(data,&v)
			if sh,ok:=Ships[v.V];ok{owned:=false;for _,o:=range p.Owned{if o==v.V{owned=true;break}}
				if owned{p.Send(map[string]interface{}{"t":"msg","v":"Already owned!"})}else if p.Gold<sh.Price{
					p.Send(map[string]interface{}{"t":"msg","v":"Not enough gold!"})}else{
					p.Gold-=sh.Price;p.Owned=append(p.Owned,v.V)
					p.Send(map[string]interface{}{"t":"msg","v":"Bought "+sh.Name+"!"})
					p.Send(map[string]interface{}{"t":"upd","gold":p.Gold,"owned":p.Owned})}}
		case "equip":
			var v struct{V string`json:"v"`};json.Unmarshal(data,&v)
			owned:=false;for _,o:=range p.Owned{if o==v.V{owned=true;break}}
			if owned{p.Ship=v.V;sh:=Ships[v.V];p.MHP,p.HP=sh.HP,sh.HP;p.Cargo=make(map[string]int);p.CargoUsed=0
				p.Send(map[string]interface{}{"t":"upd","ship":v.V,"hp":p.HP,"mhp":p.MHP})}
		case "tp":var tm struct{X,Z float64};json.Unmarshal(data,&tm);p.BX,p.BZ,p.CX,p.CZ,p.BS=tm.X,tm.Z,tm.X,tm.Z,0;if !p.OnBoat{p.OnBoat,p.Swim=true,false}
		case "deposit":
			if !p.OnBoat&&math.Hypot(p.CX-p.BX,p.CZ-p.BZ)<EMBARK{
				for ore,qty:=range p.Inv{if qty>0{p.Cargo[ore]+=qty;p.CargoUsed+=qty*10;delete(p.Inv,ore)}}
				p.Send(map[string]interface{}{"t":"msg","v":"Deposited!"})}
		case "repair":
			if !p.OnBoat{sh:=Ships[p.Ship];dmg:=p.MHP-p.HP;if dmg>0{
				cost:=dmg*sh.RepairCostPerHP;if p.Gold>=cost{p.Gold-=cost;p.HP=p.MHP
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Repaired! -%dg",cost)})
					p.Send(map[string]interface{}{"t":"upd","gold":p.Gold})}else{
					p.Send(map[string]interface{}{"t":"msg","v":"Not enough gold!"})}}}
		case "craft":
			var cm struct{Recipe string`json:"recipe"`};json.Unmarshal(data,&cm)
			if recipe,ok:=Recipes[cm.Recipe];ok&&!p.OnBoat{
				// Check materials (from cargo + inv)
				canCraft:=true
				for mat,need:=range recipe.Needs{have:=p.Cargo[mat]+p.Inv[mat]+p.CraftedItems[mat];if have<need{canCraft=false;break}}
				if canCraft{
					for mat,need:=range recipe.Needs{
						// Take from inv first, then cargo, then crafted
						left:=need
						if p.Inv[mat]>0{take:=min(p.Inv[mat],left);p.Inv[mat]-=take;left-=take;if p.Inv[mat]<=0{delete(p.Inv,mat)}}
						if left>0&&p.Cargo[mat]>0{take:=min(p.Cargo[mat],left);p.Cargo[mat]-=take;p.CargoUsed-=take*10;left-=take;if p.Cargo[mat]<=0{delete(p.Cargo,mat)}}
						if left>0&&p.CraftedItems[mat]>0{take:=min(p.CraftedItems[mat],left);p.CraftedItems[mat]-=take;left-=take;if p.CraftedItems[mat]<=0{delete(p.CraftedItems,mat)}}
					}
					p.CraftedItems[recipe.Result]++
					p.Send(map[string]interface{}{"t":"msg","v":"Crafted "+recipe.Name+"!"})
				}else{p.Send(map[string]interface{}{"t":"msg","v":"Missing materials!"})}}
		case "buyGood":
			var bm struct{Good string;Qty,Idx int};json.Unmarshal(data,&bm)
			if !p.OnBoat{var isl*Island;if bm.Idx==-1{isl=safeIsland}else if bm.Idx>=0&&bm.Idx<len(tradeIslands){isl=tradeIslands[bm.Idx]}
				if isl!=nil{if ig,ok:=isl.Goods[bm.Good];ok{if gd,ok:=Goods[bm.Good];ok{
					cap:=Ships[p.Ship].Cargo;if cap==0{break};mx:=bm.Qty;if ig.Stock<mx{mx=ig.Stock}
					if av:=(cap-p.CargoUsed)/gd.Size;av<mx{mx=av};if mx<=0{break}
					cost:=mx*ig.Price;if p.Gold<cost{break};p.Gold-=cost;ig.Stock-=mx;p.Cargo[bm.Good]+=mx;p.CargoUsed+=mx*gd.Size
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Bought %d %s",mx,gd.Name)})}}}}
		case "sellGood":
			var sm struct{Good string;Qty,Idx int};json.Unmarshal(data,&sm)
			if !p.OnBoat{var isl*Island;if sm.Idx==-1{isl=safeIsland}else if sm.Idx>=0&&sm.Idx<len(tradeIslands){isl=tradeIslands[sm.Idx]}
				if isl!=nil{if ig,ok:=isl.Goods[sm.Good];ok{have:=p.Cargo[sm.Good];sell:=sm.Qty;if have<sell{sell=have};if sell<=0{break}
					pr:=int(float64(ig.Price)*0.7) // sell at 70% of local price (profit by buying cheap elsewhere)
					p.Gold+=sell*pr;p.Cargo[sm.Good]-=sell;if p.Cargo[sm.Good]<=0{delete(p.Cargo,sm.Good)}
					p.CargoUsed-=sell*Goods[sm.Good].Size;p.addXP(sell*3)
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Sold %d for %dg +%dxp",sell,sell*pr,sell*3)})}}}
		case "sellOre":
			var sm struct{Ore string;Qty int};json.Unmarshal(data,&sm)
			if od,ok:=Ores[sm.Ore];ok&&!p.OnBoat{have:=p.Inv[sm.Ore]+p.Cargo[sm.Ore];sell:=sm.Qty;if have<sell{sell=have};if sell>0{
				fi:=p.Inv[sm.Ore];if fi>sell{fi=sell};fc:=sell-fi
				p.Inv[sm.Ore]-=fi;if p.Inv[sm.Ore]<=0{delete(p.Inv,sm.Ore)}
				p.Cargo[sm.Ore]-=fc;if p.Cargo[sm.Ore]<=0{delete(p.Cargo,sm.Ore)};p.CargoUsed-=fc*10;p.Gold+=sell*od.Value
				p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Sold %d %s for %dg",sell,od.Name,sell*od.Value)})}}
		case "sellCrafted":
			var sm struct{Item string;Qty int};json.Unmarshal(data,&sm)
			if recipe,ok:=Recipes[sm.Item];ok&&!p.OnBoat{have:=p.CraftedItems[sm.Item];sell:=sm.Qty;if have<sell{sell=have};if sell>0{
				p.CraftedItems[sm.Item]-=sell;if p.CraftedItems[sm.Item]<=0{delete(p.CraftedItems,sm.Item)}
				p.Gold+=sell*recipe.SellPrice
				p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Sold %d %s for %dg",sell,recipe.Name,sell*recipe.SellPrice)})}}
		case "buyIllegal":
			var bm struct{Item string;Qty int};json.Unmarshal(data,&bm)
			if il,ok:=Illegals[bm.Item];ok&&!p.OnBoat{
				// Must be at hidden port
				if math.Hypot(p.CX-hiddenPort.X,p.CZ-hiddenPort.Z)<hiddenPort.R{
					cap:=Ships[p.Ship].Cargo;mx:=bm.Qty;if av:=(cap-p.CargoUsed)/il.Size;av<mx{mx=av};if mx<=0{break}
					cost:=mx*il.BuyPrice;if p.Gold<cost{break};p.Gold-=cost;p.Cargo[bm.Item]+=mx;p.CargoUsed+=mx*il.Size
					p.Wanted+=mx*10 // increase wanted for smuggling
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Bought %d %s (illegal!)",mx,il.Name)})}}
		case "sellIllegal":
			var sm struct{Item string;Qty int};json.Unmarshal(data,&sm)
			if il,ok:=Illegals[sm.Item];ok&&!p.OnBoat{
				// Can sell at any trade island (risky) or hidden port (safe)
				have:=p.Cargo[sm.Item];sell:=sm.Qty;if have<sell{sell=have};if sell<=0{break}
				p.Cargo[sm.Item]-=sell;if p.Cargo[sm.Item]<=0{delete(p.Cargo,sm.Item)};p.CargoUsed-=sell*il.Size
				p.Gold+=sell*il.SellPrice
				p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Sold %d %s for %dg",sell,il.Name,sell*il.SellPrice)})}
		case "mine":
			if !p.OnBoat&&!p.Mining{land:=onLand(p.CX,p.CZ);if land!=nil&&land.Ore!=""{p.Mining=true;p.MineT=now()}}
		case "critterGold":p.Gold+=5;p.addXP(1)
		case "skillUp":
			var sm struct{Skill string`json:"skill"`};json.Unmarshal(data,&sm)
			if p.SkillPts<=0{break}
			switch sm.Skill{
			case "range":if p.SkillRange<5{p.SkillRange++;p.SkillPts--;p.Send(map[string]interface{}{"t":"msg","v":"Range upgraded!"})}
			case "trade":if p.SkillTrade<5{p.SkillTrade++;p.SkillPts--;p.Send(map[string]interface{}{"t":"msg","v":"Trade upgraded!"})}
			case "speed":if p.SkillSpeed<5{p.SkillSpeed++;p.SkillPts--;p.Send(map[string]interface{}{"t":"msg","v":"Speed upgraded!"})}
			case "hp":if p.SkillHP<5{p.SkillHP++;p.SkillPts--;p.MHP=Ships[p.Ship].HP+p.SkillHP*20;p.HP=p.MHP
				p.Send(map[string]interface{}{"t":"msg","v":"HP upgraded!"})}
			}
		case "buyHouse":
			var hm struct{Slot int;Type string};json.Unmarshal(data,&hm)
			if hm.Slot>=0&&hm.Slot<len(houseSlots){hs:=houseSlots[hm.Slot];hd,ok:=HouseDefs[hm.Type]
				if ok{if hs.Owner==""{if p.Gold>=hd.Price{p.Gold-=hd.Price;hs.Owner=p.ID;hs.Type=hm.Type;hs.Storage=make(map[string]int)
					p.Send(map[string]interface{}{"t":"msg","v":"Bought "+hd.Name+"!"});p.Send(map[string]interface{}{"t":"upd","gold":p.Gold})}}else if hs.Owner==p.ID{
					if p.Gold>=hd.Price{p.Gold-=hd.Price;hs.Type=hm.Type
						p.Send(map[string]interface{}{"t":"msg","v":"Upgraded!"});p.Send(map[string]interface{}{"t":"upd","gold":p.Gold})}}}}
		case "houseDeposit":
			var dm struct{Slot int};json.Unmarshal(data,&dm)
			if dm.Slot>=0&&dm.Slot<len(houseSlots){hs:=houseSlots[dm.Slot];if hs.Owner==p.ID{
				for k,v:=range p.Cargo{hs.Storage[k]+=v;hs.Used+=v;delete(p.Cargo,k)};p.CargoUsed=0
				for k,v:=range p.Inv{hs.Storage[k]+=v;hs.Used+=v;delete(p.Inv,k)}
				for k,v:=range p.CraftedItems{hs.Storage[k]+=v;hs.Used+=v;delete(p.CraftedItems,k)}
				hd:=HouseDefs[hs.Type];if hs.Used>hd.Cap{hs.Used=hd.Cap}
				p.Send(map[string]interface{}{"t":"msg","v":"Deposited!"})
				p.Send(map[string]interface{}{"t":"hs","slot":dm.Slot,"storage":hs.Storage,"used":hs.Used,"cap":hd.Cap})}}
		case "houseWithdraw":
			var wm struct{Slot int;Item string;Qty int};json.Unmarshal(data,&wm)
			if wm.Slot>=0&&wm.Slot<len(houseSlots){hs:=houseSlots[wm.Slot];if hs.Owner==p.ID{
				have:=hs.Storage[wm.Item];take:=wm.Qty;if have<take{take=have};if take>0{
					hs.Storage[wm.Item]-=take;if hs.Storage[wm.Item]<=0{delete(hs.Storage,wm.Item)};hs.Used-=take
					p.Cargo[wm.Item]+=take;p.CargoUsed+=take*10
					p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Took %d %s",take,wm.Item)})}}}
		case "getHS":
			var hm struct{Slot int};json.Unmarshal(data,&hm)
			if hm.Slot>=0&&hm.Slot<len(houseSlots){hs:=houseSlots[hm.Slot];if hs.Owner==p.ID{
				hd:=HouseDefs[hs.Type];p.Send(map[string]interface{}{"t":"hs","slot":hm.Slot,"storage":hs.Storage,"used":hs.Used,"cap":hd.Cap})}}
		case "mktList":
			var lm struct{Item string;Qty,Price int};json.Unmarshal(data,&lm)
			have:=p.Cargo[lm.Item]+p.Inv[lm.Item]+p.CraftedItems[lm.Item];if have<lm.Qty{break}
			left:=lm.Qty
			if p.Cargo[lm.Item]>0{take:=min(p.Cargo[lm.Item],left);p.Cargo[lm.Item]-=take;p.CargoUsed-=take*10;left-=take;if p.Cargo[lm.Item]<=0{delete(p.Cargo,lm.Item)}}
			if left>0&&p.Inv[lm.Item]>0{take:=min(p.Inv[lm.Item],left);p.Inv[lm.Item]-=take;left-=take;if p.Inv[lm.Item]<=0{delete(p.Inv,lm.Item)}}
			if left>0&&p.CraftedItems[lm.Item]>0{take:=min(p.CraftedItems[lm.Item],left);p.CraftedItems[lm.Item]-=take;left-=take;if p.CraftedItems[lm.Item]<=0{delete(p.CraftedItems,lm.Item)}}
			g.mktID++;g.market=append(g.market,MarketListing{g.mktID,p.ID,p.Name,lm.Item,lm.Qty,lm.Price})
			p.Send(map[string]interface{}{"t":"msg","v":"Listed!"})
		case "mktBuy":
			var bm struct{ID int};json.Unmarshal(data,&bm)
			for i,m:=range g.market{if m.ID==bm.ID{cost:=m.Qty*m.Price;if p.Gold<cost{break};p.Gold-=cost
				p.Cargo[m.Item]+=m.Qty;p.CargoUsed+=m.Qty*10
				if s,ok:=g.players[m.Seller];ok{s.Gold+=cost}
				g.market=append(g.market[:i],g.market[i+1:]...)
				p.Send(map[string]interface{}{"t":"msg","v":fmt.Sprintf("Bought %d %s!",m.Qty,m.Item)});break}}
		case "getMkt":
			ml:=make([]map[string]interface{},len(g.market))
			for i,m:=range g.market{ml[i]=map[string]interface{}{"id":m.ID,"seller":m.SName,"item":m.Item,"qty":m.Qty,"price":m.Price}}
			p.Send(map[string]interface{}{"t":"mkt","list":ml})
		case "chat":
			var cm struct{V string `json:"v"`};json.Unmarshal(data,&cm)
			if len(cm.V)>0{if len(cm.V)>100{cm.V=cm.V[:100]}
				chatMsg,_:=json.Marshal(map[string]interface{}{"t":"chat","n":p.Name,"v":cm.V})
				for _,op:=range g.players{op.mu.Lock();op.Conn.SetWriteDeadline(time.Now().Add(50*time.Millisecond));op.Conn.WriteMessage(websocket.TextMessage,chatMsg);op.mu.Unlock()}}
		}
		g.mu.Unlock()
	}
	g.mu.Lock();delete(g.players,id);g.mu.Unlock()
	g.mu.RLock();d,_:=json.Marshal(map[string]interface{}{"t":"left","id":id})
	for _,op:=range g.players{op.mu.Lock();op.Conn.WriteMessage(websocket.TextMessage,d);op.mu.Unlock()};g.mu.RUnlock()
}

func min(a,b int)int{if a<b{return a};return b}

func main(){
	rand.Seed(time.Now().UnixNano());initWorld()
	game:=NewGame()
	go func(){tick:=time.NewTicker(time.Second/TICK);for range tick.C{game.tick()}}()
	http.Handle("/",http.FileServer(http.Dir("public")))
	http.HandleFunc("/ws",game.handleWS)
	http.HandleFunc("/healthz",func(w http.ResponseWriter,r*http.Request){w.Write([]byte("OK"))})
	port:=os.Getenv("PORT");if port==""{port="3000"}
	log.Printf("Krew3D Go on :%s",port);log.Fatal(http.ListenAndServe(":"+port,nil))
}
