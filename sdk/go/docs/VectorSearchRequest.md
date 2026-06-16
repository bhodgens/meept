# VectorSearchRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Query** | **string** |  | 
**Limitomitempty** | Pointer to **int32** |  | [optional] 
**ShardTypesomitempty** | Pointer to **NullableString** |  | [optional] 

## Methods

### NewVectorSearchRequest

`func NewVectorSearchRequest(query string, ) *VectorSearchRequest`

NewVectorSearchRequest instantiates a new VectorSearchRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewVectorSearchRequestWithDefaults

`func NewVectorSearchRequestWithDefaults() *VectorSearchRequest`

NewVectorSearchRequestWithDefaults instantiates a new VectorSearchRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetQuery

`func (o *VectorSearchRequest) GetQuery() string`

GetQuery returns the Query field if non-nil, zero value otherwise.

### GetQueryOk

`func (o *VectorSearchRequest) GetQueryOk() (*string, bool)`

GetQueryOk returns a tuple with the Query field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetQuery

`func (o *VectorSearchRequest) SetQuery(v string)`

SetQuery sets Query field to given value.


### GetLimitomitempty

`func (o *VectorSearchRequest) GetLimitomitempty() int32`

GetLimitomitempty returns the Limitomitempty field if non-nil, zero value otherwise.

### GetLimitomitemptyOk

`func (o *VectorSearchRequest) GetLimitomitemptyOk() (*int32, bool)`

GetLimitomitemptyOk returns a tuple with the Limitomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLimitomitempty

`func (o *VectorSearchRequest) SetLimitomitempty(v int32)`

SetLimitomitempty sets Limitomitempty field to given value.

### HasLimitomitempty

`func (o *VectorSearchRequest) HasLimitomitempty() bool`

HasLimitomitempty returns a boolean if a field has been set.

### GetShardTypesomitempty

`func (o *VectorSearchRequest) GetShardTypesomitempty() string`

GetShardTypesomitempty returns the ShardTypesomitempty field if non-nil, zero value otherwise.

### GetShardTypesomitemptyOk

`func (o *VectorSearchRequest) GetShardTypesomitemptyOk() (*string, bool)`

GetShardTypesomitemptyOk returns a tuple with the ShardTypesomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetShardTypesomitempty

`func (o *VectorSearchRequest) SetShardTypesomitempty(v string)`

SetShardTypesomitempty sets ShardTypesomitempty field to given value.

### HasShardTypesomitempty

`func (o *VectorSearchRequest) HasShardTypesomitempty() bool`

HasShardTypesomitempty returns a boolean if a field has been set.

### SetShardTypesomitemptyNil

`func (o *VectorSearchRequest) SetShardTypesomitemptyNil(b bool)`

 SetShardTypesomitemptyNil sets the value for ShardTypesomitempty to be an explicit nil

### UnsetShardTypesomitempty
`func (o *VectorSearchRequest) UnsetShardTypesomitempty()`

UnsetShardTypesomitempty ensures that no value is present for ShardTypesomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


