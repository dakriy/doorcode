package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// picture is a door's picture with its image and thumbnail.
type picture struct {
	connector.StoredPicture
	image     []byte
	thumbnail []byte
}

// AddPicture stores p with its image and thumbnail and returns its id.
func (s *Store) AddPicture(_ context.Context, p connector.StoredPicture, image, thumbnail []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doors[p.DoorID]; !ok {
		return 0, errNoReference
	}
	if image == nil || thumbnail == nil {
		return 0, errNil
	}
	p.ID = s.next("pictures")
	s.pictures[p.ID] = picture{StoredPicture: p, image: list(image), thumbnail: list(thumbnail)}
	return p.ID, nil
}

// Pictures returns the door's pictures, oldest first.
func (s *Store) Pictures(_ context.Context, doorID int64) ([]connector.StoredPicture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pictures := []connector.StoredPicture{}
	for _, p := range s.pictures {
		if p.DoorID == doorID {
			pictures = append(pictures, p.StoredPicture)
		}
	}
	byID(pictures, func(p connector.StoredPicture) int64 { return p.ID })
	return pictures, nil
}

// doorPicture returns the picture whose id is id when it is the door's.
func (s *Store) doorPicture(doorID, id int64) (picture, bool) {
	p, ok := s.pictures[id]
	return p, ok && p.DoorID == doorID
}

// PictureImage returns the picture's image.
func (s *Store) PictureImage(_ context.Context, doorID, id int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.doorPicture(doorID, id)
	return list(p.image), ok, nil
}

// PictureThumbnail returns the picture's thumbnail.
func (s *Store) PictureThumbnail(_ context.Context, doorID, id int64) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.doorPicture(doorID, id)
	return list(p.thumbnail), ok, nil
}

// SetPrimaryPicture makes the door's own picture its primary one.
func (s *Store) SetPrimaryPicture(_ context.Context, doorID, id int64) (bool, error) {
	return s.changeDoor(doorID, func(d *connector.StoredDoor) error {
		if _, ok := s.doorPicture(doorID, id); !ok {
			return errNoReference
		}
		d.PrimaryPictureID = &id
		return nil
	})
}

// DeletePicture removes the door's picture, clearing the primary picture
// it was.
func (s *Store) DeletePicture(_ context.Context, doorID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doorPicture(doorID, id); !ok {
		return false, nil
	}
	delete(s.pictures, id)
	d := s.doors[doorID]
	if d.PrimaryPictureID != nil && *d.PrimaryPictureID == id {
		d.PrimaryPictureID = nil
		s.doors[doorID] = d
	}
	return true, nil
}

// zoneOut is a copy of z with its doors and open-mode schedules.
func (s *Store) zoneOut(z connector.StoredZone) connector.StoredZone {
	z.OpenScheduleIDs = sortedIDs(z.OpenScheduleIDs)
	doors := []int64{}
	for link := range s.zoneDoors {
		if link.a == z.ID {
			doors = append(doors, link.b)
		}
	}
	slices.Sort(doors)
	z.DoorIDs = doors
	return z
}

// zoneNameTaken reports whether a zone other than the one whose id is id
// has name, ignoring case.
func (s *Store) zoneNameTaken(name string, id int64) bool {
	return nameTaken(s.zones, func(z connector.StoredZone) string { return z.Name }, name, id)
}

// fillZone makes doorIDs the doors of the zone whose id is id, refusing
// doors the store does not hold.
func (s *Store) fillZone(id int64, doorIDs []int64) error {
	if !allExist(s.doors, doorIDs) {
		return errNoReference
	}
	maps.DeleteFunc(s.zoneDoors, func(link pair, _ bool) bool { return link.a == id })
	for _, doorID := range doorIDs {
		s.zoneDoors[pair{id, doorID}] = true
	}
	return nil
}

// Zones returns every zone in name order.
func (s *Store) Zones(_ context.Context) ([]connector.StoredZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	zones := []connector.StoredZone{}
	for _, z := range s.zones {
		zones = append(zones, s.zoneOut(z))
	}
	byName(zones, func(z connector.StoredZone) string { return z.Name }, func(z connector.StoredZone) int64 { return z.ID })
	return zones, nil
}

// Zone returns the zone whose id is id.
func (s *Store) Zone(_ context.Context, id int64) (connector.StoredZone, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[id]
	if !ok {
		return connector.StoredZone{}, false, nil
	}
	return s.zoneOut(z), true, nil
}

// CreateZone stores z and returns its id.
func (s *Store) CreateZone(_ context.Context, z connector.StoredZone) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.zoneNameTaken(z.Name, 0) {
		return 0, errDuplicate
	}
	if !allExist(s.doors, z.DoorIDs) || !allExist(s.schedules, z.OpenScheduleIDs) {
		return 0, errNoReference
	}
	id := s.next("zones")
	s.zones[id] = connector.StoredZone{ID: id, Name: z.Name, Description: z.Description, OpenScheduleIDs: sortedIDs(z.OpenScheduleIDs)}
	return id, s.fillZone(id, z.DoorIDs)
}

// UpdateZone stores z over the zone whose id is z.ID, replacing its doors
// and open-mode schedules.
func (s *Store) UpdateZone(_ context.Context, z connector.StoredZone) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.zones[z.ID]; !ok {
		return false, nil
	}
	if s.zoneNameTaken(z.Name, z.ID) {
		return false, errDuplicate
	}
	if !allExist(s.schedules, z.OpenScheduleIDs) {
		return false, errNoReference
	}
	if err := s.fillZone(z.ID, z.DoorIDs); err != nil {
		return false, err
	}
	s.zones[z.ID] = connector.StoredZone{ID: z.ID, Name: z.Name, Description: z.Description, OpenScheduleIDs: sortedIDs(z.OpenScheduleIDs)}
	return true, nil
}

// DeleteZone removes the zone, taking it off its doors and the access
// rules that name it.
func (s *Store) DeleteZone(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.zones[id]; !ok {
		return false, nil
	}
	delete(s.zones, id)
	maps.DeleteFunc(s.zoneDoors, func(link pair, _ bool) bool { return link.a == id })
	for ruleID, r := range s.rules {
		r.ZoneIDs = slices.DeleteFunc(list(r.ZoneIDs), func(x int64) bool { return x == id })
		s.rules[ruleID] = r
	}
	return true, nil
}

// ruleCopy is a copy of r with its zones ascending.
func ruleCopy(r connector.StoredRule) connector.StoredRule {
	r.ZoneIDs = sortedIDs(r.ZoneIDs)
	return r
}

// checkRule refuses a rule naming what the store does not hold.
func (s *Store) checkRule(r connector.StoredRule) error {
	_, group := s.groups[r.GroupID]
	_, schedule := s.schedules[r.ScheduleID]
	if !group || !schedule || !allExist(s.zones, r.ZoneIDs) {
		return errNoReference
	}
	return nil
}

// Rules returns every rule in id order.
func (s *Store) Rules(_ context.Context) ([]connector.StoredRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rules := []connector.StoredRule{}
	for _, r := range s.rules {
		rules = append(rules, ruleCopy(r))
	}
	byID(rules, func(r connector.StoredRule) int64 { return r.ID })
	return rules, nil
}

// Rule returns the rule whose id is id.
func (s *Store) Rule(_ context.Context, id int64) (connector.StoredRule, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rules[id]
	return ruleCopy(r), ok, nil
}

// CreateRule stores r and returns its id.
func (s *Store) CreateRule(_ context.Context, r connector.StoredRule) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkRule(r); err != nil {
		return 0, err
	}
	r = ruleCopy(r)
	r.ID = s.next("rules")
	s.rules[r.ID] = r
	return r.ID, nil
}

// UpdateRule stores r over the rule whose id is r.ID.
func (s *Store) UpdateRule(_ context.Context, r connector.StoredRule) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[r.ID]; !ok {
		return false, nil
	}
	if err := s.checkRule(r); err != nil {
		return false, err
	}
	s.rules[r.ID] = ruleCopy(r)
	return true, nil
}

// DeleteRule removes the rule whose id is id.
func (s *Store) DeleteRule(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.rules[id]
	delete(s.rules, id)
	return ok, nil
}

// overrideOut is o with its creator's display name.
func (s *Store) overrideOut(o connector.StoredOverride) connector.StoredOverride {
	o.CreatorName = s.users[o.CreatedBy].DisplayName
	return o
}

// DoorOverrides returns the door's overrides that end after endsAfter, by
// start and then id.
func (s *Store) DoorOverrides(_ context.Context, doorID int64, endsAfter time.Time) ([]connector.StoredOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	overrides := []connector.StoredOverride{}
	for _, o := range s.overrides {
		if o.DoorID == doorID && o.End.After(endsAfter) {
			overrides = append(overrides, s.overrideOut(o))
		}
	}
	slices.SortFunc(overrides, func(x, y connector.StoredOverride) int {
		if c := x.Start.Compare(y.Start); c != 0 {
			return c
		}
		return cmp.Compare(x.ID, y.ID)
	})
	return overrides, nil
}

// Override returns the override whose id is id.
func (s *Store) Override(_ context.Context, id int64) (connector.StoredOverride, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.overrides[id]
	if !ok {
		return connector.StoredOverride{}, false, nil
	}
	return s.overrideOut(o), true, nil
}

// CreateOverride stores o and returns its id.
func (s *Store) CreateOverride(_ context.Context, o connector.StoredOverride) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, door := s.doors[o.DoorID]
	_, user := s.users[o.CreatedBy]
	if !door || !user {
		return 0, errNoReference
	}
	o.ID = s.next("overrides")
	o.CreatorName = ""
	s.overrides[o.ID] = o
	return o.ID, nil
}

// UpdateOverride stores o's kind, start, end, and reason.
func (s *Store) UpdateOverride(_ context.Context, o connector.StoredOverride) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.overrides[o.ID]
	if !ok {
		return false, nil
	}
	stored.Kind = o.Kind
	stored.Start = o.Start
	stored.End = o.End
	stored.Reason = o.Reason
	s.overrides[o.ID] = stored
	return true, nil
}

// DeleteOverride removes the override whose id is id.
func (s *Store) DeleteOverride(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.overrides[id]
	delete(s.overrides, id)
	return ok, nil
}

// credentialCopy is a copy of c.
func credentialCopy(c connector.StoredCredential) connector.StoredCredential {
	c.UsedAt = clone(c.UsedAt)
	c.RevokedAt = clone(c.RevokedAt)
	return c
}

// CreateCredential stores c and returns its id.
func (s *Store) CreateCredential(_ context.Context, c connector.StoredCredential) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doors[c.DoorID]; !ok {
		return 0, errNoReference
	}
	for _, other := range s.credentials {
		if other.Username == c.Username {
			return 0, errDuplicate
		}
	}
	c = credentialCopy(c)
	c.ID = s.next("credentials")
	s.credentials[c.ID] = c
	return c.ID, nil
}

// credentialsWhere returns the credentials keep holds for, by door id and
// then id.
func (s *Store) credentialsWhere(keep func(connector.StoredCredential) bool) []connector.StoredCredential {
	s.mu.Lock()
	defer s.mu.Unlock()
	credentials := []connector.StoredCredential{}
	for _, c := range s.credentials {
		if keep(c) {
			credentials = append(credentials, credentialCopy(c))
		}
	}
	slices.SortFunc(credentials, func(x, y connector.StoredCredential) int {
		if x.DoorID != y.DoorID {
			return cmp.Compare(x.DoorID, y.DoorID)
		}
		return cmp.Compare(x.ID, y.ID)
	})
	return credentials
}

// DoorCredentials returns the door's credentials, oldest first.
func (s *Store) DoorCredentials(_ context.Context, doorID int64) ([]connector.StoredCredential, error) {
	return s.credentialsWhere(func(c connector.StoredCredential) bool { return c.DoorID == doorID }), nil
}

// LiveCredentials returns every credential that is not revoked.
func (s *Store) LiveCredentials(_ context.Context) ([]connector.StoredCredential, error) {
	return s.credentialsWhere(func(c connector.StoredCredential) bool { return c.RevokedAt == nil }), nil
}

// changeCredential applies change to the credential whose id is id.
func (s *Store) changeCredential(id int64, change func(c *connector.StoredCredential)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.credentials[id]
	if !ok {
		return false, nil
	}
	change(&c)
	s.credentials[id] = c
	return true, nil
}

// UseCredential stores at as when the credential was first used.
func (s *Store) UseCredential(_ context.Context, id int64, at time.Time) (bool, error) {
	return s.changeCredential(id, func(c *connector.StoredCredential) { c.UsedAt = &at })
}

// RevokeCredential stores at as when the credential was revoked.
func (s *Store) RevokeCredential(_ context.Context, id int64, at time.Time) (bool, error) {
	return s.changeCredential(id, func(c *connector.StoredCredential) { c.RevokedAt = &at })
}
