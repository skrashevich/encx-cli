package agenttools

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPacedSpacesOutConcurrentRequests(t *testing.T) {
	engine := playableEngine()
	interval := 20 * time.Millisecond
	paced := NewPaced(engine, interval)

	// An LLM fires the tool calls of one turn in parallel. Without pacing the
	// engine sees a burst and answers with its anti-spam page, which the player
	// reads as a broken session.
	const callers = 4
	var wg sync.WaitGroup
	start := time.Now()
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = paced.GetGameModel(context.Background(), 42)
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	if want := time.Duration(callers-1) * interval; elapsed < want {
		t.Fatalf("%d concurrent calls finished in %s, too fast to have been paced (want >= %s)",
			callers, elapsed, want)
	}
	if got := countCalls(engine, "GetGameModel"); got != callers {
		t.Fatalf("pacing must not drop calls, got %d of %d", got, callers)
	}
}

func TestPacedStopsWaitingWhenCancelled(t *testing.T) {
	paced := NewPaced(playableEngine(), time.Hour)

	// Warm the limiter so the next caller has to wait.
	if _, err := paced.GetProfile(context.Background()); err != nil {
		t.Fatalf("the first call should pass straight through: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := paced.GetProfile(ctx); err == nil {
		t.Fatal("a cancelled turn should not sit in the pacing queue")
	}
}

func TestPacedCoversEveryEngineCall(t *testing.T) {
	engine := playableEngine()
	paced := NewPaced(engine, time.Millisecond)
	ctx := context.Background()

	// Any unpaced method would reopen the burst that trips the anti-spam wall.
	_, _ = paced.GetDomainGames(ctx)
	_, _ = paced.GetGameList(ctx)
	_, _ = paced.GetGameModel(ctx, 42)
	_, _ = paced.GetGameModelLevel(ctx, 42, 3)
	_, _ = paced.GetGameStatistics(ctx, 42)
	_, _ = paced.GetTimeoutToGame(ctx, 42)
	_, _ = paced.GetProfile(ctx)
	_, _ = paced.GetTeamManagementInfo(ctx, 11)
	_, _ = paced.FetchResource(ctx, "/a.png")
	_, _ = paced.EnterGame(ctx, 42)
	_, _ = paced.SendCode(ctx, 42, 77, 3, "a")
	_, _ = paced.SendBonusCode(ctx, 42, 77, 3, "a")
	_, _ = paced.GetPenaltyHint(ctx, 42, 9)

	if calls := engine.recorded(); len(calls) != 13 {
		t.Fatalf("every engine method should be forwarded, got %d: %v", len(calls), calls)
	}
}
