package app

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/override"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/settings"
)

// The changes below are the ones that change what doors hold. Each makes
// its change and then sends the doors it touched what is new; the change
// stands when sending fails, which is logged, and the periodic work sends
// timelines again later.

// affecting makes change, a change to the kind of thing whose id is id,
// zero for one change creates, and sends a new timeline to every door that
// depended on it before or depends on it after. idOf reads the thing's id
// from what change returns.
func affecting[T any](ctx context.Context, a *App, kind string, id int64, change func() (T, error), idOf func(T) int64) (T, error) {
	before, beforeErr := a.rules.Affected(ctx, rule.Subject{Kind: kind, ID: id})
	changed, err := change()
	if err != nil {
		return changed, err
	}
	after, afterErr := a.rules.Affected(ctx, rule.Subject{Kind: kind, ID: idOf(changed)})
	err = cmp.Or(beforeErr, afterErr)
	if err == nil {
		err = a.pushes.Timelines(ctx, check.Distinct(append(before, after...)))
	}
	a.sendingFailed(err)
	return changed, nil
}

// removing is affecting for a change that removes the thing whose id is
// id.
func removing(ctx context.Context, a *App, kind string, id int64, change func() error) error {
	_, err := affecting(ctx, a, kind, id, func() (int64, error) { return id, change() }, func(id int64) int64 { return id })
	return err
}

// sendingFailed logs err, which says why doors could not be sent what
// changed, unless it is nil.
func (a *App) sendingFailed(err error) {
	if err != nil {
		a.log.Error("could not send doors what changed", connector.Field{Key: "error", Value: err.Error()})
	}
}

func overrideID(o connector.StoredOverride) int64 { return o.ID }

func (a *App) newOverride(ctx context.Context, doorID, creator int64, f override.Fields) (connector.StoredOverride, error) {
	return affecting(ctx, a, rule.OfOverride, 0, func() (connector.StoredOverride, error) {
		return a.overrides.Create(ctx, doorID, creator, f)
	}, overrideID)
}

func (a *App) changeOverride(ctx context.Context, doorID, id int64, f override.Fields) (connector.StoredOverride, error) {
	return affecting(ctx, a, rule.OfOverride, id, func() (connector.StoredOverride, error) {
		return a.overrides.Update(ctx, doorID, id, f)
	}, overrideID)
}

func (a *App) endOverride(ctx context.Context, doorID, id int64) (connector.StoredOverride, error) {
	return affecting(ctx, a, rule.OfOverride, id, func() (connector.StoredOverride, error) {
		return a.overrides.Cancel(ctx, doorID, id)
	}, overrideID)
}

func (a *App) removeOverride(ctx context.Context, doorID, id int64) error {
	return removing(ctx, a, rule.OfOverride, id, func() error { return a.overrides.Delete(ctx, doorID, id) })
}

func (a *App) changeSchedule(ctx context.Context, id int64, f schedule.Fields) (connector.StoredSchedule, error) {
	return affecting(ctx, a, rule.OfSchedule, id, func() (connector.StoredSchedule, error) {
		return a.schedules.Update(ctx, id, f)
	}, func(s connector.StoredSchedule) int64 { return s.ID })
}

// changeDoor changes the door whose id is id and sends it its timeline,
// which its open-mode schedules decide.
func (a *App) changeDoor(ctx context.Context, id int64, f door.Fields) (connector.StoredDoor, error) {
	d, err := a.doors.Update(ctx, id, f)
	if err == nil {
		a.sendingFailed(a.pushes.Timelines(ctx, []int64{id}))
	}
	return d, err
}

// configureDoor makes overrides the configuration overrides of the door
// whose id is id and sends it its configuration.
func (a *App) configureDoor(ctx context.Context, id int64, overrides map[string]string) (connector.StoredDoor, error) {
	d, err := a.doors.Configure(ctx, id, overrides)
	if err == nil {
		a.sendingFailed(a.pushes.Configs(ctx, []int64{id}))
	}
	return d, err
}

// settingsChanged sends every door its configuration when one of the
// settings whose keys are keys is a door configuration default, and its
// timeline when one is the site timezone.
func (a *App) settingsChanged(ctx context.Context, keys []string) {
	var errs []error
	if slices.ContainsFunc(keys, settings.Configures) {
		errs = append(errs, a.pushes.AllConfigs(ctx))
	}
	if slices.ContainsFunc(keys, settings.Reschedules) {
		errs = append(errs, a.pushes.AllTimelines(ctx))
	}
	a.sendingFailed(errors.Join(errs...))
}
