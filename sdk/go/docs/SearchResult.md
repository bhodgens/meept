# SearchResult

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Type** | **string** |  | 
**Id** | **string** |  | 
**Title** | **string** |  | 
**Snippet** | **string** |  | 
**Relevance** | **float32** |  | 

## Methods

### NewSearchResult

`func NewSearchResult(type_ string, id string, title string, snippet string, relevance float32, ) *SearchResult`

NewSearchResult instantiates a new SearchResult object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewSearchResultWithDefaults

`func NewSearchResultWithDefaults() *SearchResult`

NewSearchResultWithDefaults instantiates a new SearchResult object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetType

`func (o *SearchResult) GetType() string`

GetType returns the Type field if non-nil, zero value otherwise.

### GetTypeOk

`func (o *SearchResult) GetTypeOk() (*string, bool)`

GetTypeOk returns a tuple with the Type field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetType

`func (o *SearchResult) SetType(v string)`

SetType sets Type field to given value.


### GetId

`func (o *SearchResult) GetId() string`

GetId returns the Id field if non-nil, zero value otherwise.

### GetIdOk

`func (o *SearchResult) GetIdOk() (*string, bool)`

GetIdOk returns a tuple with the Id field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetId

`func (o *SearchResult) SetId(v string)`

SetId sets Id field to given value.


### GetTitle

`func (o *SearchResult) GetTitle() string`

GetTitle returns the Title field if non-nil, zero value otherwise.

### GetTitleOk

`func (o *SearchResult) GetTitleOk() (*string, bool)`

GetTitleOk returns a tuple with the Title field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTitle

`func (o *SearchResult) SetTitle(v string)`

SetTitle sets Title field to given value.


### GetSnippet

`func (o *SearchResult) GetSnippet() string`

GetSnippet returns the Snippet field if non-nil, zero value otherwise.

### GetSnippetOk

`func (o *SearchResult) GetSnippetOk() (*string, bool)`

GetSnippetOk returns a tuple with the Snippet field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSnippet

`func (o *SearchResult) SetSnippet(v string)`

SetSnippet sets Snippet field to given value.


### GetRelevance

`func (o *SearchResult) GetRelevance() float32`

GetRelevance returns the Relevance field if non-nil, zero value otherwise.

### GetRelevanceOk

`func (o *SearchResult) GetRelevanceOk() (*float32, bool)`

GetRelevanceOk returns a tuple with the Relevance field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetRelevance

`func (o *SearchResult) SetRelevance(v float32)`

SetRelevance sets Relevance field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


