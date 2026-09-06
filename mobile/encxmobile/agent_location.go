package encxmobile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
)

// locationToolName is the tool the model calls to learn where the device is.
const locationToolName = "enc_device_location"

// locationRequestTimeout bounds the wait for a GPS fix. A cold fix outdoors
// takes seconds; a minute means the host is not going to answer — the player
// dismissed the permission prompt or the chat — and the turn should move on
// instead of hanging until the player cancels it.
const locationRequestTimeout = time.Minute

// locationReply is the host's answer to one location request: either a JSON
// payload describing the position or a human-readable failure.
type locationReply struct {
	payloadJSON string
	errMessage  string
}

// locationTool asks the host app for the device's current GPS position.
//
// The fix has to come from the host: CoreLocation is unreachable from Go, and
// the OS permission prompt must be raised by the app process. The tool blocks
// its call the same way the confirmation gate does — delegate notification,
// then a wait on a per-request channel resolved by ResolveLocation/FailLocation.
type locationTool struct {
	session *AgentSession
}

func (t *locationTool) Name() string { return locationToolName }

func (t *locationTool) Description() string {
	return "Get the device's current GPS location. Returns latitude and longitude " +
		"in decimal degrees (WGS 84) plus the horizontal accuracy in meters. " +
		"Call it when the player asks about nearby places, distances or directions " +
		"relative to where they are. The first call may take a while: the OS may " +
		"ask the player for permission and the device needs a GPS fix."
}

func (t *locationTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *locationTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	payload, err := t.session.requestLocation(ctx)
	if err != nil {
		return toolshared.ErrorResult(err.Error()).WithError(err)
	}
	return toolshared.SilentResult(payload)
}

// requestLocation notifies the host and blocks until it answers, the turn is
// cancelled or the request times out.
func (s *AgentSession) requestLocation(ctx context.Context) (string, error) {
	// Reuse the observation layer's call ID so the host can tie the request to
	// the activity row it is already showing.
	requestID, ok := callIDFrom(ctx)
	if !ok {
		requestID = s.nextCallID()
	}

	s.mu.Lock()
	delegate := s.delegate
	if delegate == nil {
		s.mu.Unlock()
		return "", errors.New("no location delegate is attached to the session")
	}
	turn := s.turn
	waiter := make(chan locationReply, 1)
	s.pendingLocation[requestID] = waiter
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pendingLocation, requestID)
		s.mu.Unlock()
	}()

	delegate.OnLocationRequest(requestID, turn)

	timer := time.NewTimer(locationRequestTimeout)
	defer timer.Stop()

	select {
	case reply := <-waiter:
		if reply.errMessage != "" {
			return "", errors.New(reply.errMessage)
		}
		return reply.payloadJSON, nil
	case <-timer.C:
		return "", errors.New("the device did not report its location in time")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ResolveLocation delivers the device position for a pending location request.
// locationJSON is passed to the model verbatim; the host decides the fields
// (latitude, longitude, accuracy and so on).
func (s *AgentSession) ResolveLocation(requestID string, locationJSON string) error {
	if strings.TrimSpace(locationJSON) == "" {
		return errors.New("encxmobile: the location payload is empty")
	}
	return s.finishLocation(requestID, locationReply{payloadJSON: locationJSON})
}

// FailLocation reports that the host cannot provide a position — permission
// denied, location services off, or no fix. The message reaches the model, so
// it should say why in plain words.
func (s *AgentSession) FailLocation(requestID string, message string) error {
	if strings.TrimSpace(message) == "" {
		message = "the device could not determine its location"
	}
	return s.finishLocation(requestID, locationReply{errMessage: message})
}

func (s *AgentSession) finishLocation(requestID string, reply locationReply) error {
	s.mu.Lock()
	waiter, ok := s.pendingLocation[requestID]
	if ok {
		delete(s.pendingLocation, requestID)
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("encxmobile: no location request is pending for %q", requestID)
	}
	waiter <- reply
	return nil
}
