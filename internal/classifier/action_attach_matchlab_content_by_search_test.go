package classifier

import (
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/bitmagnet-io/bitmagnet/internal/classifier/classification"
    "github.com/bitmagnet-io/bitmagnet/internal/model"
)

func TestMatchlabClient(t *testing.T){
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.URL.Path!="/match"||r.URL.Query().Get("title")!="Example.Show.S01E02"{
            t.Errorf("unexpected MatchLab request: %s",r.URL.String())
        }
        w.Header().Set("Content-Type","application/json")
        fmt.Fprint(w,`{"sid_start":"tt1234567:1:2","sid_end":"tt1234567:1:2","asid_start":null,"asid_end":null}`)
    }))
    defer server.Close()
    client,err:=newMatchlabClient(server.URL)
    if err!=nil{t.Fatal(err)}
    imdb,found,err:=client.match(context.Background(),"Example.Show.S01E02")
    if err!=nil||!found||imdb!="tt1234567"{t.Fatalf("got %q, %v, %v",imdb,found,err)}
}

func TestMatchlabUnmatched(t *testing.T){
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        fmt.Fprint(w,`{"sid_start":null,"sid_end":null,"asid_start":null,"asid_end":null}`)
    }))
    defer server.Close()
    client,_:=newMatchlabClient(server.URL)
    _,found,err:=client.match(context.Background(),"Unknown")
    if err!=nil||found{t.Fatalf("expected no match: %v, %v",found,err)}
}

func TestMatchlabRangeAndInvalidConfig(t *testing.T){
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        fmt.Fprint(w,`{"sid_start":"tt1234567:1:1","sid_end":"tt1234567:1:5","asid_start":null,"asid_end":null}`)
    }))
    defer server.Close()
    client,_:=newMatchlabClient(server.URL)
    _,found,err:=client.match(context.Background(),"Pack")
    if err!=nil||!found{t.Fatalf("expected IMDb identity from same-title range: %v, %v",found,err)}
    if _,err:=newMatchlabClient("ftp://example.org");err==nil{t.Fatal("expected invalid scheme")}
    if client,err:=newMatchlabClient("");err!=nil||client!=nil{t.Fatal("empty URL must disable MatchLab")}
}

func TestMatchlabParserOverridesExistingFields(t *testing.T){
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.URL.Path!="/parse"{t.Errorf("unexpected path: %s",r.URL.Path)}
        fmt.Fprint(w,`{"title":"Example Show","year":2024,"seasons":[2],"episodes":[3,4]}`)
    }))
    defer server.Close()
    client,err:=newMatchlabClient(server.URL)
    if err!=nil{t.Fatal(err)}
    cl:=classification.Result{}
    cl.BaseTitle=model.NewNullString("Incorrect Title")
    cl.Episodes=model.Episodes{1:map[int]struct{}{8:{}}}
    client.overrideParsed(context.Background(),"Example.Show.S02E03-E04",&cl)
    if cl.BaseTitle.String!="Example Show"||cl.Date.Year!=2024||!cl.Episodes.HasEpisode(2,3)||!cl.Episodes.HasEpisode(2,4)||cl.Episodes.HasEpisode(1,8){
        t.Fatalf("parser overrides not applied: %+v",cl)
    }
}
