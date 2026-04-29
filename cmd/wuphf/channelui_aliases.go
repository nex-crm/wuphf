package main

import "github.com/nex-crm/wuphf/cmd/wuphf/channelui"

// Type aliases bridge the old package-main names to the new channelui
// package while the channel cluster is incrementally extracted. These
// aliases preserve every existing field access, method receiver, and
// composite-literal usage in the rest of cmd/wuphf so each extraction
// PR can move types without churning every callsite.
//
// The aliases will be removed once the channel cluster fully lives in
// channelui (final cleanup PR).
type (
	brokerReaction     = channelui.BrokerReaction
	brokerMessageUsage = channelui.BrokerMessageUsage
	brokerMessage      = channelui.BrokerMessage
	renderedLine       = channelui.RenderedLine
	threadedMessage    = channelui.ThreadedMessage
)
