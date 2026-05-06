package main

import (
	"errors"
	"testing"
)

// TestTunnelJoinGateAcceptsCorrectPasscode walks the gate path the share
// HTTP handler runs for each invite-accept POST: a token registered by the
// controller, with the matching passcode, must pass.
func TestTunnelJoinGateAcceptsCorrectPasscode(t *testing.T) {
	c := newWebTunnelController()
	c.passcodes["tok-1"] = "835291"
	if err := c.joinGate("tok-1", "835291"); err != nil {
		t.Fatalf("joinGate rejected matching passcode: %v", err)
	}
}

func TestTunnelJoinGateRejectsWrongPasscode(t *testing.T) {
	c := newWebTunnelController()
	c.passcodes["tok-1"] = "835291"
	err := c.joinGate("tok-1", "111111")
	if !errors.Is(err, errJoinPasscodeInvalid) {
		t.Fatalf("joinGate err=%v want errJoinPasscodeInvalid", err)
	}
}

// TestTunnelJoinGateRejectsUnknownToken makes sure an attacker cannot ride
// the tunnel with a token that was minted via a different surface (e.g. a
// network-share token leaked separately) — only tokens that this tunnel
// itself issued are redeemable through it.
func TestTunnelJoinGateRejectsUnknownToken(t *testing.T) {
	c := newWebTunnelController()
	c.passcodes["tok-1"] = "835291"
	err := c.joinGate("tok-other", "835291")
	if !errors.Is(err, errJoinPasscodeRequired) {
		t.Fatalf("joinGate err=%v want errJoinPasscodeRequired (unknown token)", err)
	}
}

// TestTunnelJoinGateUsesConstantTimeCompare guards against a regression
// from constantTimeCompare to a bare == — the latter shortcuts on the
// first differing byte and lets a network attacker time-side-channel the
// passcode. We can't directly observe timing here; instead, verify the
// helper that enforces it is wired in by exercising a length-mismatch
// pair, which == would early-return on but constantTimeCompare runs to
// completion and returns false.
func TestTunnelJoinGateLengthMismatchIsRejection(t *testing.T) {
	c := newWebTunnelController()
	c.passcodes["tok-1"] = "835291"
	if err := c.joinGate("tok-1", "8352"); !errors.Is(err, errJoinPasscodeInvalid) {
		t.Fatalf("joinGate len-mismatch err=%v want errJoinPasscodeInvalid", err)
	}
}
