package inv

import (
	"github.com/df-mc/atomic"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"reflect"
	"unsafe"
	_ "unsafe"
)

// Menu is a menu that can be sent to a player. It can be used to display a custom inventory to a player.
type Menu struct {
	name        string
	kind        byte
	submittable Submittable
	items       []item.Stack
	pos         cube.Pos

	windowID byte
}

// NewMenu creates a new menu with the submittable passed, the name passed and the kind passed.
func NewMenu(submittable Submittable, name string, kind byte) Menu {
	return Menu{name: name, submittable: submittable, kind: kind}
}

// WithStacks sets the stacks of the menu to the stacks passed.
func (m Menu) WithStacks(stacks ...item.Stack) Menu {
	m.items = stacks
	return m
}

// Submittable is a type that can be implemented by a Menu to be called when a menu is submitted.
type Submittable interface {
	Submit(p *player.Player, it item.Stack)
}

// Closer is a type that can be implemented by a Submittable to be called when a menu is closed.
type Closer interface {
	Close(p *player.Player)
}

// SendMenu sends a menu to a player. The menu passed will be displayed to the player
func SendMenu(p *player.Player, m Menu) {
	sendMenu(p, m, false)
}

// UpdateMenu updates the menu that the player passed is currently viewing to the menu passed.
func UpdateMenu(p *player.Player, m Menu) {
	sendMenu(p, m, true)
}

func sendMenu(p *player.Player, m Menu, update bool) {
	s := player_session(p)

	inv := inventory.New(len(m.items), func(slot int, before, after item.Stack) {})
	inv.Handle(handler{p: p, menu: m})
	for i, it := range m.items {
		_ = inv.SetItem(i, it)
	}
	pos := cube.PosFromVec3(p.Rotation().Vec3().Mul(-2).Add(p.Position()))
	blockPos := blockPosToProtocol(pos)
	var nextID byte

	if update {
		mn, ok := lastMenu(s)
		if ok {
			pos = mn.pos
			nextID = mn.windowID
		}
	} else {
		if m, ok := lastMenu(s); ok && m.pos != pos {
			closeLastMenu(p, m)
		}
		nextID = session_nextWindowID(s)
	}
	s.ViewBlockUpdate(pos, blockFromContainerKind(m.kind), 0)
	s.ViewBlockUpdate(pos.Add(cube.Pos{0, 1}), block.Air{}, 0)

	data := createFakeInventoryNBT(m.name, m.kind)
	data["x"], data["y"], data["z"] = blockPos.X(), blockPos.Y(), blockPos.Z()
	session_writePacket(s, &packet.BlockActorData{
		Position: blockPos,
		NBTData:  data,
	})

	updatePrivateField(s, "openedPos", *atomic.NewValue(containerPos))
	updatePrivateField(s, "openedWindow", *atomic.NewValue(inv))

	updatePrivateField(s, "containerOpened", *atomic.NewBool(true))
	updatePrivateField(s, "openedContainerID", *atomic.NewUint32(uint32(nextID)))

	var containerType byte
	switch m.kind {
	case ContainerTypeChest:
		containerType = protocol.ContainerTypeContainer
	case ContainerTypeHopper:
		containerType = protocol.ContainerTypeHopper
	case ContainerTypeDropper:
		containerType = protocol.ContainerTypeDropper
	}

	session_writePacket(s, &packet.ContainerOpen{
		WindowID:                nextID,
		ContainerPosition:       blockPos,
		ContainerType:           containerType,
		ContainerEntityUniqueID: -1,
	})
	session_sendInv(s, inv, uint32(nextID))

	m.pos = pos
	m.windowID = nextID

	menuMu.Lock()
	lastMenus[s] = m
	menuMu.Unlock()
}

// blockPosToProtocol converts a cube.Pos to a protocol.BlockPos.
func blockPosToProtocol(pos cube.Pos) protocol.BlockPos {
	return protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
}

// createFakeInventoryNBT creates a map of NBT data for a fake inventory with the name passed and the inventory
func createFakeInventoryNBT(name string, kind byte) map[string]interface{} {
	m := map[string]interface{}{"CustomName": name}
	switch kind {
	case ContainerTypeChest:
		m["id"] = "Chest"
	case ContainerTypeHopper:
		m["id"] = "Hopper"
	case ContainerTypeDropper:
		m["id"] = "Dropper"
	}
	return m
}

// updatePrivateField sets a private field of a session to the value passed.
func updatePrivateField[T any](s *session.Session, name string, value T) {
	reflectedValue := reflect.ValueOf(s).Elem()
	privateFieldValue := reflectedValue.FieldByName(name)

	privateFieldValue = reflect.NewAt(privateFieldValue.Type(), unsafe.Pointer(privateFieldValue.UnsafeAddr())).Elem()

	privateFieldValue.Set(reflect.ValueOf(value))
}

// fetchPrivateField fetches a private field of a session.
func fetchPrivateField[T any](s *session.Session, name string) T {
	reflectedValue := reflect.ValueOf(s).Elem()
	privateFieldValue := reflectedValue.FieldByName(name)
	privateFieldValue = reflect.NewAt(privateFieldValue.Type(), unsafe.Pointer(privateFieldValue.UnsafeAddr())).Elem()

	return privateFieldValue.Interface().(T)
}

// noinspection ALL
//
//go:linkname player_session github.com/df-mc/dragonfly/server/player.(*Player).session
func player_session(*player.Player) *session.Session

// noinspection ALL
//
//go:linkname session_writePacket github.com/df-mc/dragonfly/server/session.(*Session).writePacket
func session_writePacket(*session.Session, packet.Packet)

// noinspection ALL
//
//go:linkname session_nextWindowID github.com/df-mc/dragonfly/server/session.(*Session).nextWindowID
func session_nextWindowID(*session.Session) byte

// noinspection ALL
//
//go:linkname session_sendInv github.com/df-mc/dragonfly/server/session.(*Session).sendInv
func session_sendInv(*session.Session, *inventory.Inventory, uint32)