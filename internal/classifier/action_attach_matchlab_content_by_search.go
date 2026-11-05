package classifier

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"

    "github.com/bitmagnet-io/bitmagnet/internal/classifier/classification"
    "github.com/bitmagnet-io/bitmagnet/internal/model"
)

// matchlabClient calls MatchLab's existing four-field /match API.
// An empty MATCHLAB_URL disables this optional integration.
type matchlabClient struct {
    endpoint string
    httpClient *http.Client
}

type matchlabResponse struct {
    SIDStart *string `json:"sid_start"`
    SIDEnd *string `json:"sid_end"`
    ASIDStart *string `json:"asid_start"`
    ASIDEnd *string `json:"asid_end"`
}

func newMatchlabClient(raw string) (*matchlabClient,error) {
    raw=strings.TrimSpace(raw)
    if raw=="" {return nil,nil}
    u,err:=url.Parse(raw)
    if err!=nil||u.Host==""||(u.Scheme!="http"&&u.Scheme!="https")||u.User!=nil||u.RawQuery!=""||u.Fragment!="" {
        return nil,fmt.Errorf("MATCHLAB_URL must be an HTTP(S) base URL without credentials, query or fragment")
    }
    u.Path=strings.TrimRight(u.Path,"/")+"/match"
    return &matchlabClient{endpoint:u.String(),httpClient:&http.Client{
        Timeout:5*time.Second,
        CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
    }},nil
}

func (m *matchlabClient) match(ctx context.Context,title string)(string,bool,error){
    if m==nil {return "",false,nil}
    u,err:=url.Parse(m.endpoint)
    if err!=nil{return "",false,err}
    q:=u.Query();q.Set("title",title);u.RawQuery=q.Encode()
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil)
    if err!=nil{return "",false,err}
    res,err:=m.httpClient.Do(req)
    if err!=nil{return "",false,err}
    defer res.Body.Close()
    if res.StatusCode!=http.StatusOK{return "",false,fmt.Errorf("MatchLab HTTP %d",res.StatusCode)}
    var payload matchlabResponse
    if err=json.NewDecoder(io.LimitReader(res.Body,16*1024)).Decode(&payload);err!=nil {
        return "",false,fmt.Errorf("invalid MatchLab response: %w",err)
    }
    if payload.SIDStart==nil||payload.SIDEnd==nil{return "",false,nil}
    start,end:=*payload.SIDStart,*payload.SIDEnd
    if end=="" {return "",false,nil}
    endID,_,_:=strings.Cut(end,":")
    if endID!=strings.SplitN(start,":",2)[0]{return "",false,nil}
    imdb,_,_:=strings.Cut(start,":")
    if len(imdb)<9||len(imdb)>12||!strings.HasPrefix(imdb,"tt") {return "",false,errors.New("invalid IMDb identifier from MatchLab")}
    for _,digit:=range imdb[2:] {if digit<'0'||digit>'9'{return "",false,errors.New("invalid IMDb identifier from MatchLab")}}
    return imdb,true,nil
}

const attachMatchlabContentBySearchName="attach_matchlab_content_by_search"

type attachMatchlabContentBySearchAction struct{}
func (attachMatchlabContentBySearchAction) name()string{return attachMatchlabContentBySearchName}
var attachMatchlabContentBySearchPayloadSpec=payloadLiteral[string]{
    literal:attachMatchlabContentBySearchName,
    description:"Identify a torrent using the optional MatchLab API and attach IMDb content",
}
func (attachMatchlabContentBySearchAction) compileAction(ctx compilerContext)(action,error){
    if _,err:=attachMatchlabContentBySearchPayloadSpec.Unmarshal(ctx);err!=nil{return action{},ctx.error(err)}
    return action{run:func(ctx executionContext)(classification.Result,error){
        cl:=ctx.result
        if ctx.matchlabClient==nil{return cl,classification.ErrUnmatched}
        imdb,found,err:=ctx.matchlabClient.match(ctx.Context,ctx.torrent.Name)
        if err!=nil{return cl,classification.ErrUnmatched}
        if !found{return cl,classification.ErrUnmatched}
        contentType:=cl.ContentType.ContentType
        if contentType!=model.ContentTypeTvShow&&contentType!=model.ContentTypeMovie {
            return cl,classification.ErrUnmatched
        }
        // MatchLab alone determines identity. TMDB is queried strictly by IMDb ID
        // for richer metadata; no title-based TMDB search is performed.
        content:=model.Content{Type:contentType,Source:model.SourceImdb,ID:imdb,Title:ctx.torrent.Name}
        if cl.BaseTitle.Valid {content.Title=cl.BaseTitle.String}
        ref:=model.ContentRef{Type:contentType,Source:model.SourceImdb,ID:imdb}
        if ctx.tmdbClient!=nil {
            if tmdbID,lookupErr:=ctx.tmdbGetTMDBIDByExternalID(ref);lookupErr==nil {
                var details model.Content
                var detailsErr error
                if contentType==model.ContentTypeTvShow {
                    details,detailsErr=ctx.tmdbGetTVShowByTMDBID(tmdbID)
                } else {
                    details,detailsErr=ctx.tmdbGetMovieByTMDBID(tmdbID)
                }
                if detailsErr==nil {
                    content=details
                }
            }
        }
        cl.AttachContent(&content)
        return cl,nil
    }},nil
}
func (attachMatchlabContentBySearchAction) JSONSchema()JSONSchema{
    return attachMatchlabContentBySearchPayloadSpec.JSONSchema()
}

func matchlabFromEnvironment()(*matchlabClient,error){
    return newMatchlabClient(os.Getenv("MATCHLAB_URL"))
}
