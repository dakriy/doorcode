package demo

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// AdminPassword is the password the demo gives the first administrator.
const AdminPassword = "doorcode-demo"

// enrollmentPassword is the password the simulated controllers enroll
// with.
const enrollmentPassword = "demo-enrollment"

// Site is what Seed made: the administrator's sign-in, the doors, and the
// codes people use at them.
type Site struct {
	AdminEmail string
	// Doors are the doors a simulated controller is enrolled as, and
	// Waiting the controller that waits for approval.
	Doors   []SimulatedDoor
	Waiting SimulatedDoor
	// Codes are the codes of the users who have one.
	Codes []string
	// start is when the history begins.
	start time.Time
}

// SimulatedDoor is a controller the demo plays: its hardware id, the
// firmware it runs, its signal, and the door it is approved as (0 for
// none).
type SimulatedDoor struct {
	HardwareID string
	Firmware   string
	Signal     int
	DoorID     int64
}

// The schedules the demo creates, by id: the migration seeds Always as 1.
const (
	always        = 1
	businessHours = 2
	weekends      = 3
	lobbyHours    = 4
	nightShift    = 5
)

var schedules = []map[string]any{
	{"name": "Business hours", "windows": []map[string]any{{"days": weekdays, "start": "07:00", "end": "19:00"}}},
	{"name": "Weekends", "windows": []map[string]any{{"days": []string{"sat", "sun"}, "start": "09:00", "end": "17:00"}}},
	{"name": "Lobby open hours", "windows": []map[string]any{{"days": weekdays, "start": "08:30", "end": "11:30"}, {"days": weekdays, "start": "13:30", "end": "16:00"}}},
	{"name": "Night shift", "windows": []map[string]any{{"days": []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}, "start": "22:00", "end": "06:00"}}},
}

var weekdays = []string{"mon", "tue", "wed", "thu", "fri"}

// demoDoor is a door the demo creates and how its pictures look.
type demoDoor struct {
	body       map[string]any
	wall, door color.RGBA
}

// The doors, numbered from 1 in this order.
var doors = []demoDoor{
	{map[string]any{"name": "Main entrance", "location": "Building A, ground floor", "public": true,
		"notes": "Open to visitors while reception is staffed."}, color.RGBA{R: 226, G: 220, B: 208, A: 255}, color.RGBA{R: 52, G: 92, B: 160, A: 255}},
	{map[string]any{"name": "Staff entrance", "location": "Building A, rear courtyard", "public": true},
		color.RGBA{R: 180, G: 90, B: 70, A: 255}, color.RGBA{R: 60, G: 120, B: 80, A: 255}},
	{map[string]any{"name": "Server room", "location": "Building A, basement", "notes": "Security only. Call the on-call engineer before entering."},
		color.RGBA{R: 200, G: 204, B: 210, A: 255}, color.RGBA{R: 90, G: 94, B: 104, A: 255}},
	{map[string]any{"name": "Workshop", "location": "Building B", "public": true},
		color.RGBA{R: 214, G: 196, B: 160, A: 255}, color.RGBA{R: 170, G: 60, B: 50, A: 255}},
	{map[string]any{"name": "Loading dock", "location": "Building B, north side"},
		color.RGBA{R: 150, G: 150, B: 150, A: 255}, color.RGBA{R: 210, G: 160, B: 40, A: 255}},
	{map[string]any{"name": "Bike storage", "location": "Courtyard", "public": true, "notes": "Its controller is waiting to be approved."},
		color.RGBA{R: 120, G: 160, B: 120, A: 255}, color.RGBA{R: 70, G: 70, B: 80, A: 255}},
}

// The door ids, in the order of doors.
const (
	mainEntrance  = 1
	staffEntrance = 2
	serverRoom    = 3
	workshop      = 4
	loadingDock   = 5
)

// The zones, numbered from 1.
var zones = []map[string]any{
	{"name": "Building A", "description": "The outside doors of building A.", "door_ids": []int64{mainEntrance, staffEntrance}},
	{"name": "Building B", "description": "The workshop and its dock.", "door_ids": []int64{workshop, loadingDock}},
	{"name": "Reception", "description": "Open to visitors while reception is staffed.", "door_ids": []int64{mainEntrance},
		"open_schedule_ids": []int64{lobbyHours}},
	{"name": "Courtyard", "description": "The staff entrance on its own, for weekend access.", "door_ids": []int64{staffEntrance}},
	{"name": "Secure rooms", "description": "Rooms only security enters at any time.", "door_ids": []int64{serverRoom}},
	{"name": "Night rounds", "description": "The doors facilities checks overnight.", "door_ids": []int64{serverRoom, loadingDock}},
}

// The zone ids.
const (
	buildingA   = 1
	buildingB   = 2
	courtyard   = 4
	secureRooms = 5
	nightRounds = 6
)

// The groups, numbered from 2: the migration seeds Administrators as 1.
var groups = []map[string]any{
	{"slug": "staff", "name": "Staff", "description": "Everyone employed on site.", "roles": []string{}, "preassigned_user_ids": []string{"100310", "100311"}},
	{"slug": "security", "name": "Security", "description": "Guards; they get in everywhere and can command doors.", "roles": []string{"door-commander", "view-logs"}},
	{"slug": "contractors", "name": "Contractors", "description": "Outside crews working in building B.", "roles": []string{}},
	{"slug": "facilities", "name": "Facilities", "description": "Look after the doors, their schedules, and their firmware.",
		"roles": []string{"manage-doors", "manage-schedules", "manage-firmware"}},
	{"slug": "front-desk", "name": "Front desk", "description": "Reception; they look after people and read the log.", "roles": []string{"manage-users", "view-logs"}},
}

// The group ids.
const (
	staff       = 2
	security    = 3
	contractors = 4
	facilities  = 5
	frontDesk   = 6
)

// person is a user the demo creates, numbered from firstPerson: the first
// administrator is 1.
type person struct {
	first, last, userID, code string
	groups                    []int64
	// expired says the account ended a week ago.
	expired bool
}

var people = []person{
	{"Ann", "Smith", "100234", "482913", []int64{staff}, false},
	{"Ben", "Carter", "100235", "573024", []int64{staff}, false},
	{"Chloe", "Davis", "100236", "690135", []int64{security}, false},
	{"Dev", "Patel", "100237", "714246", []int64{staff, facilities}, false},
	{"Eva", "Novak", "100238", "825357", []int64{contractors}, true},
	{"Frank", "Ito", "100239", "936468", []int64{contractors}, false},
	{"Grace", "Lee", "100240", "", []int64{staff}, false},
	{"Hugo", "Brandt", "100241", "147579", []int64{staff, frontDesk}, false},
	{"Ivy", "Chen", "100242", "258680", []int64{staff}, false},
}

// firstPerson is the user id of the first of people.
const firstPerson = 2

// The user ids of people, in its order.
const (
	ann = firstPerson + iota
	ben
	chloe
	dev
	eva
	frank
	grace
	hugo
	ivy
)

// hasExpired reports whether the person whose user id is user is marked
// expired.
func hasExpired(user int64) bool {
	return people[user-firstPerson].expired
}

// expiredAt is when the account of every person marked expired ended: a
// week before now.
func expiredAt(now time.Time) time.Time {
	return now.AddDate(0, 0, -7)
}

// The access rules, numbered from 1.
var rules = []map[string]any{
	{"group_id": staff, "schedule_id": businessHours, "zone_ids": []int64{buildingA}},
	{"group_id": staff, "schedule_id": weekends, "zone_ids": []int64{courtyard}},
	{"group_id": security, "schedule_id": always, "zone_ids": []int64{buildingA, buildingB, secureRooms}},
	{"group_id": contractors, "schedule_id": businessHours, "zone_ids": []int64{buildingB}},
	{"group_id": facilities, "schedule_id": nightShift, "zone_ids": []int64{nightRounds}},
}

// The firmware versions, oldest first; the last is the default.
var firmwares = []struct{ version, notes string }{
	{"1.3.2", "Keypad debounce fix."},
	{"1.4.0", "Telemetry reports free memory and reconnects."},
	{"1.4.1", "Faster reconnects after a broker restart."},
}

// waiting is the controller that announces itself and waits for approval.
var waiting = SimulatedDoor{HardwareID: "a4cf12e0b1f6", Firmware: "1.4.1", Signal: -66}

// Seed signs in as the first administrator with the password it logged,
// gives them AdminPassword, and builds the demo site through the API:
// settings, schedules, doors with pictures, zones, groups, users with
// codes, access rules, overrides, firmware, and access requests. Then it
// writes weeks of history into store. It runs before Run, so the
// controllers find the enrollment password set.
func Seed(ctx context.Context, h http.Handler, store connector.Store, log *Log, now time.Time) (Site, error) {
	password, email, found := log.Password("email")
	if !found {
		return Site{}, errors.New("the application logged no first administrator; start the demo with an empty store")
	}
	admin, err := signIn(ctx, h, email, password)
	if err != nil {
		return Site{}, err
	}
	err = admin.call(http.MethodPut, "/api/v1/me/password", map[string]string{"current_password": password, "new_password": AdminPassword}, nil)
	if err != nil {
		return Site{}, err
	}
	site := Site{AdminEmail: email, Waiting: waiting, start: now.AddDate(0, 0, -historyDays)}
	steps := []func(client, time.Time, *Site) error{seedSettings, seedSchedules, seedDoors, seedZones, seedGroups, seedPeople, seedRules, seedOverrides, seedFirmware, seedRequests}
	for _, step := range steps {
		if err := step(admin, now, &site); err != nil {
			return Site{}, fmt.Errorf("seed the demo: %w", err)
		}
	}
	if err := writeHistory(ctx, store, site, now); err != nil {
		return Site{}, fmt.Errorf("write the demo's history: %w", err)
	}
	return site, nil
}

func seedSettings(admin client, _ time.Time, _ *Site) error {
	values := [][2]string{
		{"site_name", "doorcode demo"},
		{"public_url", "https://doorcode.example.com"},
		{"enrollment_password", enrollmentPassword},
		{"door_telemetry_interval_s", "15"},
	}
	for _, v := range values {
		if err := admin.call(http.MethodPut, "/api/v1/settings/"+v[0], map[string]string{"value": v[1]}, nil); err != nil {
			return err
		}
	}
	return nil
}

// createAll posts each body to path.
func createAll(admin client, path string, bodies []map[string]any) error {
	for _, body := range bodies {
		if err := admin.call(http.MethodPost, path, body, nil); err != nil {
			return err
		}
	}
	return nil
}

func seedSchedules(admin client, _ time.Time, _ *Site) error {
	return createAll(admin, "/api/v1/schedules", schedules)
}

func seedZones(admin client, _ time.Time, _ *Site) error {
	return createAll(admin, "/api/v1/zones", zones)
}

func seedGroups(admin client, _ time.Time, _ *Site) error {
	return createAll(admin, "/api/v1/groups", groups)
}

func seedRules(admin client, _ time.Time, _ *Site) error {
	return createAll(admin, "/api/v1/access-rules", rules)
}

// seedDoors creates the doors, each with a picture of itself and the
// first two with one of their keypad too, and remembers the controllers
// to approve as their doors.
func seedDoors(admin client, _ time.Time, site *Site) error {
	for i, d := range doors {
		if err := admin.call(http.MethodPost, "/api/v1/doors", d.body, nil); err != nil {
			return err
		}
		path := fmt.Sprintf("/api/v1/doors/%d/pictures", i+1)
		pictures := [][]byte{doorPicture(d.wall, d.door)}
		if i < 2 {
			pictures = append(pictures, keypadPicture(d.wall))
		}
		for _, p := range pictures {
			if err := admin.upload(http.MethodPost, path, nil, "picture", "door.png", p, nil); err != nil {
				return err
			}
		}
	}
	for _, p := range profiles {
		site.Doors = append(site.Doors, p.controller())
	}
	return nil
}

// seedPeople creates the users, sets their codes, and adds them to their
// groups.
func seedPeople(admin client, now time.Time, site *Site) error {
	for i, p := range people {
		id := i + firstPerson
		expires := now.AddDate(2, 0, 0)
		if p.expired {
			expires = expiredAt(now)
		}
		body := map[string]any{
			"email": strings.ToLower(p.first + "." + p.last + "@example.com"), "user_id": p.userID,
			"first_name": p.first, "last_name": p.last, "expires_at": expires.UTC().Format(time.RFC3339),
		}
		if err := admin.call(http.MethodPost, "/api/v1/users", body, nil); err != nil {
			return err
		}
		if err := setCode(admin, id, p.code, site); err != nil {
			return err
		}
		for _, g := range p.groups {
			if err := admin.call(http.MethodPut, fmt.Sprintf("/api/v1/groups/%d/members/%d", g, id), nil, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func setCode(admin client, id int, code string, site *Site) error {
	if code == "" {
		return nil
	}
	site.Codes = append(site.Codes, code)
	return admin.call(http.MethodPut, fmt.Sprintf("/api/v1/users/%d/code", id), map[string]string{"code": code}, nil)
}

// seedOverrides opens the workshop for a delivery around now and locks
// the staff entrance for maintenance tomorrow morning.
func seedOverrides(admin client, now time.Time, _ *Site) error {
	hour := now.UTC().Truncate(time.Hour)
	tomorrow := hour.Truncate(24*time.Hour).AddDate(0, 0, 1)
	overrides := []struct {
		door int64
		body map[string]any
	}{
		{workshop, map[string]any{"kind": "open", "start": hour.Add(-time.Hour).Format(time.RFC3339), "end": hour.Add(3 * time.Hour).Format(time.RFC3339),
			"reason": "Delivery of the new lathe."}},
		{staffEntrance, map[string]any{"kind": "locked", "start": tomorrow.Add(6 * time.Hour).Format(time.RFC3339), "end": tomorrow.Add(8 * time.Hour).Format(time.RFC3339),
			"reason": "Floor maintenance in the courtyard."}},
	}
	for _, o := range overrides {
		if err := admin.call(http.MethodPost, fmt.Sprintf("/api/v1/doors/%d/overrides", o.door), o.body, nil); err != nil {
			return err
		}
	}
	return nil
}

// seedFirmware uploads the versions, makes the newest the default, and
// keeps the workshop on the one before.
func seedFirmware(admin client, _ time.Time, _ *Site) error {
	for _, f := range firmwares {
		image := []byte(fmt.Sprintf("doorcode demo controller firmware %s\n", f.version))
		fields := map[string]string{"version": f.version, "notes": f.notes}
		if err := admin.upload(http.MethodPost, "/api/v1/firmware", fields, "image", "firmware.bin", image, nil); err != nil {
			return err
		}
	}
	newest := len(firmwares)
	if err := admin.call(http.MethodPut, "/api/v1/firmware/default", map[string]int{"firmware_id": newest}, nil); err != nil {
		return err
	}
	return admin.call(http.MethodPut, fmt.Sprintf("/api/v1/doors/%d/firmware", workshop), map[string]int{"firmware_id": newest - 1}, nil)
}

// seedRequests has three people ask for access, and answers two of them,
// resolving one.
func seedRequests(admin client, _ time.Time, _ *Site) error {
	asks := []struct {
		user        int64
		title, body string
		reply       string
		resolve     bool
	}{
		{grace, "Workshop access", "I am helping with the lathe installation next week and need to get into the workshop.", "", false},
		{ivy, "Weekend access", "Could I get into the staff entrance on Saturdays? I am covering the weekend shift.",
			"Done: staff can now use the staff entrance on weekends.", true},
		{frank, "Loading dock", "Our crew unloads at the dock from 6:00; could contractors get in from then?",
			"We are checking with security first.", false},
	}
	for i, a := range asks {
		var token struct{ Secret string }
		if err := admin.call(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/tokens", a.user), map[string]any{"name": "demo"}, &token); err != nil {
			return err
		}
		if err := admin.as(token.Secret).call(http.MethodPost, "/api/v1/me/requests", map[string]string{"title": a.title, "body": a.body}, nil); err != nil {
			return err
		}
		if err := answer(admin, i+1, a.reply, a.resolve); err != nil {
			return err
		}
	}
	return nil
}

func answer(admin client, id int, reply string, resolve bool) error {
	if reply != "" {
		if err := admin.call(http.MethodPut, fmt.Sprintf("/api/v1/requests/%d/reply", id), map[string]string{"reply": reply}, nil); err != nil {
			return err
		}
	}
	if !resolve {
		return nil
	}
	return admin.call(http.MethodPost, fmt.Sprintf("/api/v1/requests/%d/resolve", id), nil, nil)
}
