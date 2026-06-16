# SearchRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Query** | **string** |  | 
**Scopeomitempty** | Pointer to **string** |  | [optional] 
**Limitomitempty** | Pointer to **int32** |  | [optional] 

## Methods

### NewSearchRequest

`func NewSearchRequest(query string, ) *SearchRequest`

NewSearchRequest instantiates a new SearchRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewSearchRequestWithDefaults

`func NewSearchRequestWithDefaults() *SearchRequest`

NewSearchRequestWithDefaults instantiates a new SearchRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetQuery

`func (o *SearchRequest) GetQuery() string`

GetQuery returns the Query field if non-nil, zero value otherwise.

### GetQueryOk

`func (o *SearchRequest) GetQueryOk() (*string, bool)`

GetQueryOk returns a tuple with the Query field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetQuery

`func (o *SearchRequest) SetQuery(v string)`

SetQuery sets Query field to given value.


### GetScopeomitempty

`func (o *SearchRequest) GetScopeomitempty() string`

GetScopeomitempty returns the Scopeomitempty field if non-nil, zero value otherwise.

### GetScopeomitemptyOk

`func (o *SearchRequest) GetScopeomitemptyOk() (*string, bool)`

GetScopeomitemptyOk returns a tuple with the Scopeomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetScopeomitempty

`func (o *SearchRequest) SetScopeomitempty(v string)`

SetScopeomitempty sets Scopeomitempty field to given value.

### HasScopeomitempty

`func (o *SearchRequest) HasScopeomitempty() bool`

HasScopeomitempty returns a boolean if a field has been set.

### GetLimitomitempty

`func (o *SearchRequest) GetLimitomitempty() int32`

GetLimitomitempty returns the Limitomitempty field if non-nil, zero value otherwise.

### GetLimitomitemptyOk

`func (o *SearchRequest) GetLimitomitemptyOk() (*int32, bool)`

GetLimitomitemptyOk returns a tuple with the Limitomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLimitomitempty

`func (o *SearchRequest) SetLimitomitempty(v int32)`

SetLimitomitempty sets Limitomitempty field to given value.

### HasLimitomitempty

`func (o *SearchRequest) HasLimitomitempty() bool`

HasLimitomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


