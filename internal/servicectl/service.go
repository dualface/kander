package servicectl

import (
	"context"
	"errors"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/svcinstall"
)

// service returns the server service that runs in place of a detached server
// for this process. A Controller marked Unmanaged never uses one.
func (c Controller) service(ctx context.Context) (svcinstall.Installed, bool, error) {
	if c.Unmanaged {
		return svcinstall.Installed{}, false, nil
	}
	return svcinstall.ManagedFor(ctx, svcinstall.KindServer)
}

// direct is the controller that acts on the server process itself.
func (c Controller) direct() Controller {
	c.Unmanaged = true
	return c
}

// serviceTarget is the URL the server of a service definition answers at.
func serviceTarget(svc svcinstall.Installed) (string, error) {
	listen, _ := svc.Spec.Flag("--listen")
	return ProbeURL(listen)
}

// ServiceListen and ServiceDataDir read the server options a service definition records.
func ServiceListen(spec svcinstall.Spec) string {
	listen, _ := spec.Flag("--listen")
	return listen
}

func ServiceDataDir(spec svcinstall.Spec) string {
	dir, _ := spec.Flag("--data")
	return dir
}

// startService starts the server through its service manager. A data directory
// or listen address the user gave that differs from the definition is refused:
// the service manager would run its own server, not the one asked for.
func (c Controller) startService(ctx context.Context, svc svcinstall.Installed, o StartOptions) (api.ServerInfo, error) {
	data, listen := ServiceDataDir(svc.Spec), ServiceListen(svc.Spec)
	if o.ExplicitData {
		dir, err := DataDir(o.DataDir)
		if err != nil {
			return api.ServerInfo{}, err
		}
		if !sameDirectory(dir, data) {
			return api.ServerInfo{}, errors.New(config.Text("service.server_flag_conflict", "--data", dir, data, svc.Location))
		}
	}
	if o.ExplicitListen && o.Listen != listen {
		return api.ServerInfo{}, errors.New(config.Text("service.server_flag_conflict", "--listen", o.Listen, listen, svc.Location))
	}
	target, err := ProbeURL(listen)
	if err != nil {
		return api.ServerInfo{}, err
	}
	if healthy(ctx, target) {
		return Info(ctx, target)
	}
	if err := svcinstall.Start(ctx, svcinstall.KindServer); err != nil {
		return api.ServerInfo{}, err
	}
	return c.awaitService(ctx, svc, target)
}

// awaitService waits until the server of the service answers as ready.
func (c Controller) awaitService(ctx context.Context, svc svcinstall.Installed, target string) (api.ServerInfo, error) {
	wait, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	for {
		if healthy(wait, target) {
			if info, err := Info(wait, target); err == nil {
				return info, nil
			}
		}
		select {
		case <-wait.Done():
			return api.ServerInfo{}, errors.New(config.Text("service.server_not_ready", target, svc.Spec.LogPath))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// stopService stops the server through its service manager. On Windows the
// manager does not stop it, and the authenticated stop does.
func (c Controller) stopService(ctx context.Context, svc svcinstall.Installed) (bool, error) {
	target, err := serviceTarget(svc)
	if err != nil {
		return false, err
	}
	_, probe := Info(ctx, target)
	running := !client.IsUnreachable(probe)
	err = svcinstall.Stop(ctx, svcinstall.KindServer)
	if errors.Is(err, svcinstall.ErrProtocolStop) {
		return c.direct().Stop(ctx, target)
	}
	if err != nil {
		return running, err
	}
	return running, c.awaitGone(ctx, target)
}

// awaitGone waits until nothing answers at target: launchd unloads a job after
// bootout has returned.
func (c Controller) awaitGone(ctx context.Context, target string) error {
	p, err := peer(target)
	if err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	for {
		probe, done := context.WithTimeout(wait, 300*time.Millisecond)
		_, err := p.Health(probe)
		done()
		if client.IsUnreachable(err) && wait.Err() == nil {
			return nil
		}
		select {
		case <-wait.Done():
			return errors.New(config.Text("server.stop_timeout"))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// restartService restarts the server through its service manager, with the
// options of the definition.
func (c Controller) restartService(ctx context.Context, svc svcinstall.Installed) (api.ServerInfo, error) {
	target, err := serviceTarget(svc)
	if err != nil {
		return api.ServerInfo{}, err
	}
	err = svcinstall.Restart(ctx, svcinstall.KindServer)
	if errors.Is(err, svcinstall.ErrProtocolStop) {
		if _, err := c.direct().Stop(ctx, target); err != nil {
			return api.ServerInfo{}, err
		}
		err = svcinstall.Start(ctx, svcinstall.KindServer)
	}
	if err != nil {
		return api.ServerInfo{}, err
	}
	return c.awaitService(ctx, svc, target)
}
