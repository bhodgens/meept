# CacheService

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Cache** | Pointer to **map[string]interface{}** |  | [optional] 

## Methods

### NewCacheService

`func NewCacheService() *CacheService`

NewCacheService instantiates a new CacheService object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCacheServiceWithDefaults

`func NewCacheServiceWithDefaults() *CacheService`

NewCacheServiceWithDefaults instantiates a new CacheService object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetCache

`func (o *CacheService) GetCache() map[string]interface{}`

GetCache returns the Cache field if non-nil, zero value otherwise.

### GetCacheOk

`func (o *CacheService) GetCacheOk() (*map[string]interface{}, bool)`

GetCacheOk returns a tuple with the Cache field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCache

`func (o *CacheService) SetCache(v map[string]interface{})`

SetCache sets Cache field to given value.

### HasCache

`func (o *CacheService) HasCache() bool`

HasCache returns a boolean if a field has been set.

### SetCacheNil

`func (o *CacheService) SetCacheNil(b bool)`

 SetCacheNil sets the value for Cache to be an explicit nil

### UnsetCache
`func (o *CacheService) UnsetCache()`

UnsetCache ensures that no value is present for Cache, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


