package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

func (s *Service) registerChannels() error {
	for op, h := range map[string]daemon.Handler{
		control.OpChannelsList:   {Action: auditlog.OperationConfigRead, Func: s.handleChannelsList},
		control.OpChannelsAdd:    {Action: auditlog.OperationConfigApply, Func: s.handleChannelsAdd},
		control.OpChannelsRemove: {Action: auditlog.OperationConfigApply, Func: s.handleChannelsRemove},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

func channelOf(p control.ChannelParams) config.Channel {
	return config.Channel{
		Name: p.Name, Type: p.Type, URL: p.URL, Host: p.Host, Port: p.Port, From: p.From,
		To: p.To, Security: p.Security, ChatID: p.ChatID, Credential: p.Credential,
	}
}

func paramsOf(c config.Channel) control.ChannelParams {
	return control.ChannelParams{
		Name: c.Name, Type: c.Type, URL: c.URL, Host: c.Host, Port: c.Port, From: c.From,
		To: c.To, Security: c.Security, ChatID: c.ChatID, Credential: c.Credential,
	}
}

// settingsWith returns the settings of m with the given channels and the revision of the settings file.
func settingsWith(m *config.Model, revs config.Revisions, channels []config.Channel) (config.Settings, config.Revision) {
	st := m.Settings
	if st.Schema == 0 {
		st.Schema = config.CurrentSchema
	}
	st.Channels = channels
	return st, revs[config.SettingsPath()]
}

func (s *Service) handleChannelsAdd(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := decodeParams(params, func(p control.ChannelParams) string { return p.Name })
	if err != nil {
		return nil, err
	}
	// The change is judged by the problems of its own channel; findings about the others and about the
	// rest of the settings must not block it.
	var prefix string
	return s.applyChange(c, configChange{
		target: approval.ChannelsName,
		inScope: func(pr config.Problem) bool {
			return pr.File == config.SettingsPath() && strings.HasPrefix(pr.Field, prefix)
		},
		prepare: func(m *config.Model, revs config.Revisions) (*config.Model, func() (config.Revision, error), error) {
			for _, ch := range m.Settings.Channels {
				if ch.Name == p.Name {
					return nil, nil, &control.Error{Code: control.CodeExists}
				}
			}
			prefix = fmt.Sprintf("channels[%d].", len(m.Settings.Channels))
			chans := append(append([]config.Channel(nil), m.Settings.Channels...), channelOf(p))
			st, rev := settingsWith(m, revs, chans)
			after := *m
			after.Settings = st
			return &after, func() (config.Revision, error) { return s.store.SaveSettings(st, rev) }, nil
		},
		approve: s.approvals.ApproveCurrentChannels,
	})
}

func (s *Service) handleChannelsRemove(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	var ref control.ChannelRef
	if err := json.Unmarshal(params, &ref); err != nil || !config.ValidName(ref.Name) {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	return s.applyChange(c, configChange{
		target:  approval.ChannelsName,
		inScope: func(config.Problem) bool { return false },
		prepare: func(m *config.Model, revs config.Revisions) (*config.Model, func() (config.Revision, error), error) {
			var rest []config.Channel
			found := false
			for _, ch := range m.Settings.Channels {
				if ch.Name == ref.Name {
					found = true
					continue
				}
				rest = append(rest, ch)
			}
			if !found {
				return nil, nil, &control.Error{Code: control.CodeNotFound}
			}
			st, rev := settingsWith(m, revs, rest)
			return nil, func() (config.Revision, error) { return s.store.SaveSettings(st, rev) }, nil
		},
		approve: s.approvals.ApproveCurrentChannels,
	})
}

func (s *Service) handleChannelsList(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	m, _, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	state := func(config.Channel) string { return control.StateUnknown }
	if s.vaultState() == control.VaultUnlocked {
		state = s.channelState()
	}
	out := control.ChannelsListResult{Channels: make([]control.ChannelSummary, 0, len(m.Settings.Channels))}
	for _, ch := range m.Settings.Channels {
		out.Channels = append(out.Channels, control.ChannelSummary{ChannelParams: paramsOf(ch), State: state(ch)})
	}
	sort.Slice(out.Channels, func(i, j int) bool { return out.Channels[i].Name < out.Channels[j].Name })
	return out, nil
}

// channelState returns a function that says whether a channel is part of the approved state exactly as
// stored, down to the vault id of its credential. Without a verified approved state no channel is.
func (s *Service) channelState() func(config.Channel) string {
	ap, err := s.approvals.ApprovedChannels()
	infos, ierr := s.vault.List()
	if ierr != nil {
		return func(config.Channel) string { return control.StateUnknown }
	}
	ids := map[string]string{}
	for _, in := range infos {
		ids[in.Name] = in.ID
	}
	return func(ch config.Channel) string {
		if err != nil {
			return control.StatePending
		}
		for _, a := range ap.Channels {
			if reflect.DeepEqual(a, ch) && (ch.Credential == "" || ap.Credentials[ch.Credential] == ids[ch.Credential]) {
				return control.StateApproved
			}
		}
		return control.StatePending
	}
}
