# CacheStatsResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Hits** | **int32** |  | 
**Misses** | **int32** |  | 
**Size** | **int32** |  | 

## Methods

### NewCacheStatsResponse

`func NewCacheStatsResponse(hits int32, misses int32, size int32, ) *CacheStatsResponse`

NewCacheStatsResponse instantiates a new CacheStatsResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCacheStatsResponseWithDefaults

`func NewCacheStatsResponseWithDefaults() *CacheStatsResponse`

NewCacheStatsResponseWithDefaults instantiates a new CacheStatsResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetHits

`func (o *CacheStatsResponse) GetHits() int32`

GetHits returns the Hits field if non-nil, zero value otherwise.

### GetHitsOk

`func (o *CacheStatsResponse) GetHitsOk() (*int32, bool)`

GetHitsOk returns a tuple with the Hits field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHits

`func (o *CacheStatsResponse) SetHits(v int32)`

SetHits sets Hits field to given value.


### GetMisses

`func (o *CacheStatsResponse) GetMisses() int32`

GetMisses returns the Misses field if non-nil, zero value otherwise.

### GetMissesOk

`func (o *CacheStatsResponse) GetMissesOk() (*int32, bool)`

GetMissesOk returns a tuple with the Misses field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMisses

`func (o *CacheStatsResponse) SetMisses(v int32)`

SetMisses sets Misses field to given value.


### GetSize

`func (o *CacheStatsResponse) GetSize() int32`

GetSize returns the Size field if non-nil, zero value otherwise.

### GetSizeOk

`func (o *CacheStatsResponse) GetSizeOk() (*int32, bool)`

GetSizeOk returns a tuple with the Size field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSize

`func (o *CacheStatsResponse) SetSize(v int32)`

SetSize sets Size field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


