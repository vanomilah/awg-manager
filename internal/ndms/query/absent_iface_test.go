package query

import (
	"context"
	"testing"
)

// F546: одиночный `show interface <name>` по отсутствующему имени NDMS
// пишет E «unable to find» в своём журнале. Читатели по имени обязаны
// отсекать имя, которого нет в кэше интерфейсов, не спрашивая NDMS.

func absentIfaceQueries(t *testing.T) (*Queries, *FakeGetter) {
	t.Helper()
	fg := newFakeGetter()
	fg.SetJSON(ifaceListPath, sampleIfaceList) // Wireguard7 в списке нет
	return NewQueries(Deps{Getter: fg, Logger: NopLogger()}), fg
}

func TestPeers_AbsentInterface_NoQuery(t *testing.T) {
	q, fg := absentIfaceQueries(t)
	peers, err := q.Peers.GetPeers(context.Background(), "Wireguard7")
	if err != nil || len(peers) != 0 {
		t.Fatalf("want (0 peers, nil), got (%v, %v)", peers, err)
	}
	if n := fg.Calls("/show/interface/Wireguard7") + fg.PostInterfaceCalls("Wireguard7"); n != 0 {
		t.Fatalf("show interface Wireguard7 ушёл %d раз", n)
	}
}

func TestFetchSummary_AbsentInterface_NoQuery(t *testing.T) {
	q, fg := absentIfaceQueries(t)
	d, err := q.Interfaces.FetchSummary(context.Background(), "OpkgTun12")
	if err != nil || d != nil {
		t.Fatalf("want (nil, nil), got (%#v, %v)", d, err)
	}
	if n := fg.PostInterfaceCalls("OpkgTun12"); n != 0 {
		t.Fatalf("show interface OpkgTun12 ушёл %d раз", n)
	}
}

func TestWGServers_AbsentInterface_NoQuery(t *testing.T) {
	q, fg := absentIfaceQueries(t)
	ctx := context.Background()
	_, _ = q.WGServers.GetSystemTunnel(ctx, "Wireguard7")
	if _, err := q.WGServers.Get(ctx, "Wireguard7"); err == nil {
		t.Error("Get отсутствующего сервера обязан вернуть ошибку, как прежний 404 rc")
	}
	_, _ = q.WGServers.GetConfig(ctx, "Wireguard7")
	_, _ = q.WGServers.PeersRCFresh(ctx, "Wireguard7")
	_, _ = q.WGServers.GetASCParams(ctx, "Wireguard7", true)
	n := fg.PostInterfaceCalls("Wireguard7") + fg.PostSystemNameCalls("Wireguard7") +
		fg.Calls("/show/rc/interface/Wireguard7") + fg.Calls("/show/rc/interface/Wireguard7/wireguard/asc")
	if n != 0 {
		t.Fatalf("по отсутствующему Wireguard7 ушло %d запросов", n)
	}
}

func TestResolveSystemName_AbsentInterface_NoResolver(t *testing.T) {
	q, fg := absentIfaceQueries(t)
	if got := q.Interfaces.ResolveSystemName(context.Background(), "Proxy2"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
	_ = q.Interfaces.SystemNames(context.Background(), []string{"Proxy2", "OpkgTun12"})
	if n := fg.PostSystemNameCalls("Proxy2") + fg.PostSystemNameCalls("OpkgTun12") + fg.BatchPostCalls(); n != 0 {
		t.Fatalf("резолвер спрошен %d раз по отсутствующим именам", n)
	}
}

// Refresh по неизвестному имени читает список, но кэш правит только по этому
// имени: соседа, обновлённого хуком, пока шёл запрос, старый снимок не затирает.
func TestInterfaceStore_Refresh_UnknownKeepsNeighbours(t *testing.T) {
	fg := newFakeGetter()
	fg.SetJSON(ifaceListPath, sampleIfaceList)
	s := NewInterfaceStore(fg, NopLogger())
	ctx := context.Background()
	_, _ = s.Get(ctx, "Wireguard0")
	s.OnLayerChanged("Wireguard0", "conf", "disabled") // хук после снимка

	if _, err := s.Refresh(ctx, "OpkgTun11"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, "Wireguard0")
	if got == nil || got.ConfLayer != "disabled" {
		t.Fatalf("сосед затёрт снимком списка: %#v", got)
	}
}

// Опрос статистики managed-сервера-сироты (панель открыта — каждые ~5 с) не
// должен читать весь список на каждом вызове: ошибка отсутствия в KeyedStore
// не кэшируется, поэтому отсечка — только по кэшу.
func TestWGServers_AbsentInterface_NoListPerPoll(t *testing.T) {
	q, fg := absentIfaceQueries(t)
	ctx := context.Background()
	_, _ = q.WGServers.Get(ctx, "Wireguard7") // бутстрап кэша
	before := fg.Calls(ifaceListPath)
	for i := 0; i < 3; i++ {
		_, _ = q.WGServers.Get(ctx, "Wireguard7")
		_, _ = q.WGServers.GetConfig(ctx, "Wireguard7")
	}
	if n := fg.Calls(ifaceListPath) - before; n != 0 {
		t.Fatalf("опрос сироты прочитал список %d раз", n)
	}
}
