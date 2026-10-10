// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"unicode/utf8"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
	"github.com/quentinemusee/musubee/core/persons"
)

// This file runs the persons commands. The persons store names conversations
// by their identity on their network (persons.Key), which outlives the local
// rooms and will be the same on the user's other devices; this file maps
// those identities to and from the API's conversation IDs. A link whose
// conversation is not on this device (its account was not added here, or
// was logged out) stays in the store, dormant, and is not shown.

// personKey returns the network identity of a conversation. With direct,
// only a direct conversation is accepted.
func (c *Core) personKey(ctx context.Context, conversation string, direct bool) (persons.Key, error) {
	roomID, err := c.room(ctx, conversation)
	if err != nil {
		return persons.Key{}, err
	}
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return persons.Key{}, err
	}
	br := c.host.Bridge(networkid.BridgeID(room.BridgeID))
	var portal *bridgev2.Portal
	if br != nil {
		if portal, err = br.GetPortalByMXID(ctx, roomID); err != nil {
			return persons.Key{}, err
		}
	}
	if portal == nil {
		// The room is still being created.
		return persons.Key{}, newError(api.ErrorCodeNotFound, "no conversation %q", conversation)
	}
	if direct && portal.RoomType != database.RoomTypeDM {
		return persons.Key{}, newError(api.ErrorCodeInvalidParams, "conversation %q is not a direct conversation", conversation)
	}
	return portalKey(br, portal), nil
}

func portalKey(br *bridgev2.Bridge, portal *bridgev2.Portal) persons.Key {
	return persons.Key{Network: string(br.ID), Chat: string(portal.ID), Receiver: string(portal.Receiver)}
}

// keyRoom returns the room of a linked conversation, or "" if the
// conversation is not on this device.
func (c *Core) keyRoom(ctx context.Context, key persons.Key) (id.RoomID, error) {
	network := networkid.BridgeID(key.Network)
	br := c.host.Bridge(network)
	if br == nil || !c.hasNetwork(network) {
		return "", nil
	}
	portalKey := networkid.PortalKey{ID: networkid.PortalID(key.Chat), Receiver: networkid.UserLoginID(key.Receiver)}
	portal, err := br.GetExistingPortalByKey(ctx, portalKey)
	if err != nil || portal == nil || portal.PortalKey != portalKey || portal.MXID == "" {
		return "", err
	}
	if _, err = c.host.Matrix.Room(ctx, portal.MXID); errors.Is(err, localmatrix.ErrNotFound) {
		return "", nil
	}
	return portal.MXID, err
}

// person converts a stored person. Its conversations are those on this
// device only, so it may have none.
func (c *Core) person(ctx context.Context, p *persons.Person) (api.Person, error) {
	result := api.Person{PersonID: p.ID, Name: p.Name, ConversationIds: []string{}}
	for _, key := range p.Links {
		roomID, err := c.keyRoom(ctx, key)
		if err != nil {
			return result, err
		}
		if roomID != "" {
			result.ConversationIds = append(result.ConversationIds, conversationID(roomID))
		}
	}
	return result, nil
}

func (c *Core) personsList(ctx context.Context, _ api.Empty) (api.PersonsListResult, error) {
	result := api.PersonsListResult{Persons: []api.Person{}}
	stored, err := c.persons.List(ctx)
	if err != nil {
		return result, err
	}
	for _, p := range stored {
		person, err := c.person(ctx, p)
		if err != nil {
			return result, err
		}
		if len(person.ConversationIds) > 0 {
			result.Persons = append(result.Persons, person)
		}
	}
	return result, nil
}

func (c *Core) personsCreate(ctx context.Context, p api.PersonsCreateParams) (api.PersonResult, error) {
	if len(p.ConversationIds) == 0 {
		return api.PersonResult{}, newError(api.ErrorCodeInvalidParams, "a person needs at least one conversation")
	}
	if p.Name != "" && !persons.ValidName(p.Name) {
		return api.PersonResult{}, newError(api.ErrorCodeInvalidParams, "invalid person name")
	}
	keys := make([]persons.Key, 0, len(p.ConversationIds))
	for _, conversation := range p.ConversationIds {
		key, err := c.personKey(ctx, conversation, true)
		if err != nil {
			return api.PersonResult{}, err
		}
		keys = append(keys, key)
	}
	name := p.Name
	if name == "" {
		var err error
		if name, err = c.conversationName(ctx, p.ConversationIds[0]); err != nil {
			return api.PersonResult{}, err
		}
	}
	c.personsMu.Lock()
	defer c.personsMu.Unlock()
	change, err := c.persons.Create(ctx, name, keys)
	if err != nil {
		return api.PersonResult{}, err
	}
	return c.personChanged(ctx, change, keys)
}

// conversationName returns the name of a conversation, shortened to a valid
// person name, for a person created without one.
func (c *Core) conversationName(ctx context.Context, conversation string) (string, error) {
	roomID, err := c.room(ctx, conversation)
	if err != nil {
		return "", err
	}
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return "", err
	}
	conv, err := c.conversation(ctx, room, nil)
	if err != nil {
		return "", err
	}
	name := ""
	if conv != nil {
		name = conv.Name
	}
	for len(name) > persons.MaxNameLength {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if !persons.ValidName(name) {
		return "", newError(api.ErrorCodeInvalidParams, "conversation %q has no name: give the person one", conversation)
	}
	return name, nil
}

func (c *Core) personsRename(ctx context.Context, p api.PersonsRenameParams) (api.PersonResult, error) {
	if !persons.ValidName(p.Name) {
		return api.PersonResult{}, newError(api.ErrorCodeInvalidParams, "invalid person name")
	}
	c.personsMu.Lock()
	defer c.personsMu.Unlock()
	person, err := c.persons.Rename(ctx, p.PersonID, p.Name)
	if err != nil {
		return api.PersonResult{}, err
	}
	return c.personChanged(ctx, persons.Change{Updated: []*persons.Person{person}}, nil)
}

func (c *Core) personsLink(ctx context.Context, p api.PersonsLinkParams) (api.PersonResult, error) {
	key, err := c.personKey(ctx, p.ConversationID, true)
	if err != nil {
		return api.PersonResult{}, err
	}
	c.personsMu.Lock()
	defer c.personsMu.Unlock()
	change, err := c.persons.Link(ctx, p.PersonID, key)
	if err != nil {
		return api.PersonResult{}, err
	}
	return c.personChanged(ctx, change, []persons.Key{key})
}

func (c *Core) personsUnlink(ctx context.Context, p api.PersonsUnlinkParams) (api.Empty, error) {
	key, err := c.personKey(ctx, p.ConversationID, false)
	if err != nil {
		return api.Empty{}, err
	}
	c.personsMu.Lock()
	defer c.personsMu.Unlock()
	change, err := c.persons.Unlink(ctx, key)
	if err != nil || len(change.Updated)+len(change.Deleted) == 0 {
		return api.Empty{}, err
	}
	_, err = c.personChanged(ctx, change, []persons.Key{key})
	return api.Empty{}, err
}

func (c *Core) personsDelete(ctx context.Context, p api.PersonsDeleteParams) (api.Empty, error) {
	c.personsMu.Lock()
	defer c.personsMu.Unlock()
	person, err := c.persons.Delete(ctx, p.PersonID)
	if err != nil {
		return api.Empty{}, err
	}
	_, err = c.personChanged(ctx, persons.Change{Deleted: []string{person.ID}}, person.Links)
	return api.Empty{}, err
}

// personChanged sends the events of a change: person.updated for each
// person changed, person.deleted for each person deleted or left without a
// conversation on this device, then conversation.updated for each
// conversation of moved that is on this device. It returns the first person
// of the change, if any. The caller holds personsMu, so that the events of
// two changes do not interleave.
func (c *Core) personChanged(ctx context.Context, change persons.Change, moved []persons.Key) (api.PersonResult, error) {
	var result api.PersonResult
	var events []*api.Event
	for i, p := range change.Updated {
		person, err := c.person(ctx, p)
		if err != nil {
			return result, c.missedEvents(err)
		}
		if i == 0 {
			result.Person = person
		}
		if len(person.ConversationIds) == 0 {
			events = append(events, &api.Event{Type: api.EventPersonDeleted, Data: api.PersonDeletedEvent{PersonID: person.PersonID}})
		} else {
			events = append(events, &api.Event{Type: api.EventPersonUpdated, Data: api.PersonEvent{Person: person}})
		}
	}
	for _, personID := range change.Deleted {
		events = append(events, &api.Event{Type: api.EventPersonDeleted, Data: api.PersonDeletedEvent{PersonID: personID}})
	}
	owners, err := c.persons.Owners(ctx)
	if err != nil {
		return result, c.missedEvents(err)
	}
	for _, key := range moved {
		roomID, err := c.keyRoom(ctx, key)
		if err != nil {
			return result, c.missedEvents(err)
		}
		if roomID == "" {
			continue
		}
		room, err := c.host.Matrix.Room(ctx, roomID)
		if err != nil {
			return result, c.missedEvents(err)
		}
		conv, err := c.conversation(ctx, room, owners)
		if err != nil {
			return result, c.missedEvents(err)
		}
		if conv != nil {
			events = append(events, &api.Event{Type: api.EventConversationUpdated, Data: api.ConversationEvent{Conversation: *conv}})
		}
	}
	for _, evt := range events {
		c.pushEvent(evt)
	}
	return result, nil
}

// missedEvents tells the application to read the state again when the
// events of a stored change could not be built: the change was made, and
// the error is returned to the caller.
func (c *Core) missedEvents(err error) error {
	c.push(resyncEvent)
	return err
}
