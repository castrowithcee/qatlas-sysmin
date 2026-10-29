package control

// ChannelParams are the parameters of OpChannelsAdd. Which fields apply depends on Type; the validation
// of the configuration decides. Credential names a vault credential.
type ChannelParams struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	URL        string   `json:"url,omitempty"`
	Host       string   `json:"host,omitempty"`
	Port       int      `json:"port,omitempty"`
	From       string   `json:"from,omitempty"`
	To         []string `json:"to,omitempty"`
	Security   string   `json:"security,omitempty"`
	ChatID     string   `json:"chat_id,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ChannelRef names one channel for OpChannelsRemove.
type ChannelRef struct {
	Name string `json:"name"`
}

// ChannelSummary is one channel as OpChannelsList reports it. State is StateApproved when the channel is
// part of the approved state exactly as stored, StatePending when it is not, and StateUnknown while the
// approved state cannot be compared.
type ChannelSummary struct {
	ChannelParams
	State string `json:"state"`
}

// ChannelsListResult is the answer to OpChannelsList.
type ChannelsListResult struct {
	Channels []ChannelSummary `json:"channels"`
}
