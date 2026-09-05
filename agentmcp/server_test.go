package agentmcp

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/encx-cli/agenttools"
	"github.com/skrashevich/encx-cli/encx"
)

// stubEngine is a scripted agenttools.Engine that records what the tools reached.
type stubEngine struct {
	mu    sync.Mutex
	calls []string
	model *encx.GameModel
}

func (e *stubEngine) record(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, name)
}

func (e *stubEngine) called(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, call := range e.calls {
		if call == name {
			return true
		}
	}
	return false
}

func (e *stubEngine) GetDomainGames(context.Context) ([]encx.DomainGame, error) {
	e.record("GetDomainGames")
	return nil, nil
}

func (e *stubEngine) GetGameList(context.Context, ...int) (*encx.GameListResponse, error) {
	e.record("GetGameList")
	return &encx.GameListResponse{}, nil
}

func (e *stubEngine) GetGameModel(context.Context, int, ...url.Values) (*encx.GameModel, error) {
	e.record("GetGameModel")
	return e.model, nil
}

func (e *stubEngine) GetGameModelLevel(context.Context, int, int) (*encx.GameModel, error) {
	e.record("GetGameModelLevel")
	return e.model, nil
}

func (e *stubEngine) GetGameStatistics(context.Context, int) (*encx.GameStatisticsResponse, error) {
	e.record("GetGameStatistics")
	return &encx.GameStatisticsResponse{}, nil
}

func (e *stubEngine) GetTimeoutToGame(context.Context, int) (*int, error) {
	e.record("GetTimeoutToGame")
	return nil, nil
}

func (e *stubEngine) GetProfile(context.Context) (*encx.Profile, error) {
	e.record("GetProfile")
	return &encx.Profile{Login: "player", TeamID: 11}, nil
}

func (e *stubEngine) GetTeamManagementInfo(context.Context, int) (*encx.TeamManagementInfo, error) {
	e.record("GetTeamManagementInfo")
	return &encx.TeamManagementInfo{TeamID: 11}, nil
}

func (e *stubEngine) EnterGame(context.Context, int) (string, error) {
	e.record("EnterGame")
	return "", nil
}

func (e *stubEngine) SendCode(context.Context, int, int, int, string) (*encx.GameModel, error) {
	e.record("SendCode")
	return e.model, nil
}

func (e *stubEngine) SendBonusCode(context.Context, int, int, int, string) (*encx.GameModel, error) {
	e.record("SendBonusCode")
	return e.model, nil
}

func (e *stubEngine) GetPenaltyHint(context.Context, int, int) (*encx.GameModel, error) {
	e.record("GetPenaltyHint")
	return e.model, nil
}

func (e *stubEngine) FetchResource(_ context.Context, rawURL string, _ ...encx.ResourceOptions) (*encx.Resource, error) {
	e.record("FetchResource")
	return &encx.Resource{URL: rawURL, ContentType: "image/png", Data: []byte("png-bytes")}, nil
}

func newStubEngine() *stubEngine {
	return &stubEngine{
		model: &encx.GameModel{
			GameId:    42,
			GameTitle: "Тестовая игра",
			Level: &encx.Level{
				LevelId: 77,
				Number:  3,
				Tasks:   []encx.LevelTask{{TaskText: "Найдите табличку"}},
			},
		},
	}
}

// connect wires an in-memory client to a server built over the given catalog.
func connect(t *testing.T, catalog *agenttools.Catalog, clientOpts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	server, err := NewServer(catalog, "test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, clientOpts)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func newCatalog(t *testing.T, engine agenttools.Engine, opts agenttools.Options) *agenttools.Catalog {
	t.Helper()
	catalog, err := agenttools.NewCatalog(engine, opts)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func listToolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func resultImages(result *mcp.CallToolResult) []*mcp.ImageContent {
	var images []*mcp.ImageContent
	for _, content := range result.Content {
		if image, ok := content.(*mcp.ImageContent); ok {
			images = append(images, image)
		}
	}
	return images
}

func resultText(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func TestNewServerRequiresCatalog(t *testing.T) {
	if _, err := NewServer(nil, "test"); err == nil {
		t.Fatal("a nil catalog should be rejected")
	}
}

func TestListToolsFollowsPolicy(t *testing.T) {
	readonly := connect(t, newCatalog(t, newStubEngine(), agenttools.Options{Policy: agenttools.PolicyReadonly}), nil)
	full := connect(t, newCatalog(t, newStubEngine(), agenttools.Options{Policy: agenttools.PolicyFull}), nil)

	readonlyNames := listToolNames(t, readonly)
	fullNames := listToolNames(t, full)

	if !contains(readonlyNames, "enc_game_state") {
		t.Fatalf("read tools should be published, got %v", readonlyNames)
	}
	for _, mutating := range []string{"enc_send_code", "enc_send_bonus_code", "enc_take_penalty_hint", "enc_enter_game"} {
		if contains(readonlyNames, mutating) {
			t.Fatalf("%s must not be published under the read-only policy: %v", mutating, readonlyNames)
		}
		if !contains(fullNames, mutating) {
			t.Fatalf("%s should be published under the full policy: %v", mutating, fullNames)
		}
	}
}

func TestToolsPublishSchemaAndReadOnlyHint(t *testing.T) {
	session := connect(t, newCatalog(t, newStubEngine(), agenttools.Options{Policy: agenttools.PolicyFull}), nil)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var checkedRead, checkedMutating bool
	for _, tool := range result.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s should publish an object schema, got %T", tool.Name, tool.InputSchema)
		}
		if schema["type"] != "object" {
			t.Fatalf("%s schema should have type object, got %v", tool.Name, schema["type"])
		}
		switch tool.Name {
		case "enc_game_state":
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s should be annotated read-only", tool.Name)
			}
			checkedRead = true
		case "enc_send_code":
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s must not be annotated read-only", tool.Name)
			}
			checkedMutating = true
		}
	}
	if !checkedRead || !checkedMutating {
		t.Fatal("both a read tool and a mutating tool should have been inspected")
	}
}

func TestCallReadToolReturnsEngineJSON(t *testing.T) {
	engine := newStubEngine()
	session := connect(t, newCatalog(t, engine, agenttools.Options{Policy: agenttools.PolicyReadonly}), nil)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "enc_game_state",
		Arguments: map[string]any{"game_id": 42},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("a read call should succeed, got %q", resultText(result))
	}
	if !engine.called("GetGameModel") {
		t.Fatal("the call should reach the engine")
	}
	if !strings.Contains(resultText(result), `"game_id":42`) {
		t.Fatalf("the engine payload should be returned, got %q", resultText(result))
	}
}

func TestCallPictureToolReturnsTheImageToTheClient(t *testing.T) {
	engine := newStubEngine()
	session := connect(t, newCatalog(t, engine, agenttools.Options{Policy: agenttools.PolicyReadonly}), nil)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "enc_view_image",
		Arguments: map[string]any{"url": "/upload/task.png"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("viewing a picture should succeed, got %q", resultText(result))
	}
	// A tool result that talks about a picture the client was never handed is
	// the failure the picture tools exist to remove.
	images := resultImages(result)
	if len(images) != 1 {
		t.Fatalf("the picture should travel as image content, got %d images in %d blocks",
			len(images), len(result.Content))
	}
	if images[0].MIMEType != "image/png" {
		t.Fatalf("the image content type should survive, got %q", images[0].MIMEType)
	}
	if string(images[0].Data) != "png-bytes" {
		t.Fatalf("the image bytes should be decoded, got %q", images[0].Data)
	}
	if !strings.Contains(resultText(result), `"content_type":"image/png"`) {
		t.Fatalf("the JSON description should still be there, got %q", resultText(result))
	}
}

func TestDecodeInlineImageSkipsWhatIsNotAnImage(t *testing.T) {
	for _, inline := range []string{
		"",
		"media://abc123",
		"data:image/png;base64",
		"data:image/png;base64,***",
		"data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte("riff")),
		"data:image/png,plain",
	} {
		if _, _, ok := decodeInlineImage(inline); ok {
			t.Fatalf("%.40q is not an inline image and must not become image content", inline)
		}
	}

	mimeType, data, ok := decodeInlineImage(
		"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpeg-bytes")))
	if !ok || mimeType != "image/jpeg" || string(data) != "jpeg-bytes" {
		t.Fatalf("a JPEG data URL should decode, got %q / %q / %v", mimeType, data, ok)
	}
}

func TestCallMutatingToolIsRejectedUnderReadonly(t *testing.T) {
	engine := newStubEngine()
	session := connect(t, newCatalog(t, engine, agenttools.Options{Policy: agenttools.PolicyReadonly}), nil)

	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "enc_send_code",
		Arguments: map[string]any{"game_id": 42, "code": "bravo"},
	})
	if err == nil {
		t.Fatal("calling a tool the server never published should fail")
	}
	if engine.called("SendCode") {
		t.Fatal("the engine must not be reached under the read-only policy")
	}
}

func TestApprovePolicyElicitsConfirmation(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		action    string
		confirm   bool
		wantCall  bool
		wantError bool
	}{
		{name: "accepted", action: "accept", confirm: true, wantCall: true},
		{name: "confirmed false", action: "accept", confirm: false, wantError: true},
		{name: "declined", action: "decline", wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newStubEngine()
			catalog := newCatalog(t, engine, agenttools.Options{
				Policy:    agenttools.PolicyApprove,
				Confirmer: NewElicitConfirmer(),
			})
			var elicited string
			session := connect(t, catalog, &mcp.ClientOptions{
				ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					elicited = req.Params.Message
					return &mcp.ElicitResult{
						Action:  testCase.action,
						Content: map[string]any{"confirm": testCase.confirm},
					}, nil
				},
			})

			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "enc_send_code",
				Arguments: map[string]any{"game_id": 42, "code": "bravo"},
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if !strings.Contains(elicited, "enc_send_code") {
				t.Fatalf("the confirmation prompt should name the tool, got %q", elicited)
			}
			if testCase.wantError && !result.IsError {
				t.Fatalf("an unconfirmed call should return an error result, got %q", resultText(result))
			}
			if testCase.wantCall != engine.called("SendCode") {
				t.Fatalf("engine reached = %v, want %v", engine.called("SendCode"), testCase.wantCall)
			}
		})
	}
}

func TestApprovePolicyRefusesClientsWithoutElicitation(t *testing.T) {
	engine := newStubEngine()
	catalog := newCatalog(t, engine, agenttools.Options{
		Policy:    agenttools.PolicyApprove,
		Confirmer: NewElicitConfirmer(),
	})
	session := connect(t, catalog, nil)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "enc_send_code",
		Arguments: map[string]any{"game_id": 42, "code": "bravo"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("a client that cannot confirm should not get the mutation through, got %q", resultText(result))
	}
	if engine.called("SendCode") {
		t.Fatal("the engine must not be reached without a confirmation")
	}
}

func TestConfirmerWithoutSessionRefuses(t *testing.T) {
	approved, err := NewElicitConfirmer().ConfirmToolCall(
		context.Background(),
		agenttools.ConfirmRequest{Tool: "enc_send_code"},
	)
	if approved || err == nil {
		t.Fatal("without an MCP session the confirmer should refuse")
	}
}
