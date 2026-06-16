# MemoryQueryRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Query** | **string** |  | 
**Limitomitempty** | Pointer to **int32** |  | [optional] 
**Categoryomitempty** | Pointer to **string** |  | [optional] 

## Methods

### NewMemoryQueryRequest

`func NewMemoryQueryRequest(query string, ) *MemoryQueryRequest`

NewMemoryQueryRequest instantiates a new MemoryQueryRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewMemoryQueryRequestWithDefaults

`func NewMemoryQueryRequestWithDefaults() *MemoryQueryRequest`

NewMemoryQueryRequestWithDefaults instantiates a new MemoryQueryRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetQuery

`func (o *MemoryQueryRequest) GetQuery() string`

GetQuery returns the Query field if non-nil, zero value otherwise.

### GetQueryOk

`func (o *MemoryQueryRequest) GetQueryOk() (*string, bool)`

GetQueryOk returns a tuple with the Query field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetQuery

`func (o *MemoryQueryRequest) SetQuery(v string)`

SetQuery sets Query field to given value.


### GetLimitomitempty

`func (o *MemoryQueryRequest) GetLimitomitempty() int32`

GetLimitomitempty returns the Limitomitempty field if non-nil, zero value otherwise.

### GetLimitomitemptyOk

`func (o *MemoryQueryRequest) GetLimitomitemptyOk() (*int32, bool)`

GetLimitomitemptyOk returns a tuple with the Limitomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLimitomitempty

`func (o *MemoryQueryRequest) SetLimitomitempty(v int32)`

SetLimitomitempty sets Limitomitempty field to given value.

### HasLimitomitempty

`func (o *MemoryQueryRequest) HasLimitomitempty() bool`

HasLimitomitempty returns a boolean if a field has been set.

### GetCategoryomitempty

`func (o *MemoryQueryRequest) GetCategoryomitempty() string`

GetCategoryomitempty returns the Categoryomitempty field if non-nil, zero value otherwise.

### GetCategoryomitemptyOk

`func (o *MemoryQueryRequest) GetCategoryomitemptyOk() (*string, bool)`

GetCategoryomitemptyOk returns a tuple with the Categoryomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCategoryomitempty

`func (o *MemoryQueryRequest) SetCategoryomitempty(v string)`

SetCategoryomitempty sets Categoryomitempty field to given value.

### HasCategoryomitempty

`func (o *MemoryQueryRequest) HasCategoryomitempty() bool`

HasCategoryomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


