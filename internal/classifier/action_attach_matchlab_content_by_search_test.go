package classifier

import (
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "testing"
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

func TestMatchlabRejectsRangeAndInvalidConfig(t *testing.T){
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        fmt.Fprint(w,`{"sid_start":"tt1234567:1:1","sid_end":"tt1234567:1:5","asid_start":null,"asid_end":null}`)
    }))
    defer server.Close()
    client,_:=newMatchlabClient(server.URL)
    _,found,err:=client.match(context.Background(),"Pack")
    if err!=nil||found{t.Fatalf("expected no ambiguous range match: %v, %v",found,err)}
    if _,err:=newMatchlabClient("ftp://example.org");err==nil{t.Fatal("expected invalid scheme")}
    if client,err:=newMatchlabClient("");err!=nil||client!=nil{t.Fatal("empty URL must disable MatchLab")}
}
