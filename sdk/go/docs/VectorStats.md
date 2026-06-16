# VectorStats

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**LoadedShards** | **int32** |  | 
**MaxRamShards** | **int32** |  | 
**LruHits** | **int32** |  | 
**LruMisses** | **int32** |  | 
**LruEvictions** | **int32** |  | 
**ShardDetails** | **NullableString** |  | 

## Methods

### NewVectorStats

`func NewVectorStats(loadedShards int32, maxRamShards int32, lruHits int32, lruMisses int32, lruEvictions int32, shardDetails NullableString, ) *VectorStats`

NewVectorStats instantiates a new VectorStats object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewVectorStatsWithDefaults

`func NewVectorStatsWithDefaults() *VectorStats`

NewVectorStatsWithDefaults instantiates a new VectorStats object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetLoadedShards

`func (o *VectorStats) GetLoadedShards() int32`

GetLoadedShards returns the LoadedShards field if non-nil, zero value otherwise.

### GetLoadedShardsOk

`func (o *VectorStats) GetLoadedShardsOk() (*int32, bool)`

GetLoadedShardsOk returns a tuple with the LoadedShards field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLoadedShards

`func (o *VectorStats) SetLoadedShards(v int32)`

SetLoadedShards sets LoadedShards field to given value.


### GetMaxRamShards

`func (o *VectorStats) GetMaxRamShards() int32`

GetMaxRamShards returns the MaxRamShards field if non-nil, zero value otherwise.

### GetMaxRamShardsOk

`func (o *VectorStats) GetMaxRamShardsOk() (*int32, bool)`

GetMaxRamShardsOk returns a tuple with the MaxRamShards field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetMaxRamShards

`func (o *VectorStats) SetMaxRamShards(v int32)`

SetMaxRamShards sets MaxRamShards field to given value.


### GetLruHits

`func (o *VectorStats) GetLruHits() int32`

GetLruHits returns the LruHits field if non-nil, zero value otherwise.

### GetLruHitsOk

`func (o *VectorStats) GetLruHitsOk() (*int32, bool)`

GetLruHitsOk returns a tuple with the LruHits field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLruHits

`func (o *VectorStats) SetLruHits(v int32)`

SetLruHits sets LruHits field to given value.


### GetLruMisses

`func (o *VectorStats) GetLruMisses() int32`

GetLruMisses returns the LruMisses field if non-nil, zero value otherwise.

### GetLruMissesOk

`func (o *VectorStats) GetLruMissesOk() (*int32, bool)`

GetLruMissesOk returns a tuple with the LruMisses field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLruMisses

`func (o *VectorStats) SetLruMisses(v int32)`

SetLruMisses sets LruMisses field to given value.


### GetLruEvictions

`func (o *VectorStats) GetLruEvictions() int32`

GetLruEvictions returns the LruEvictions field if non-nil, zero value otherwise.

### GetLruEvictionsOk

`func (o *VectorStats) GetLruEvictionsOk() (*int32, bool)`

GetLruEvictionsOk returns a tuple with the LruEvictions field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLruEvictions

`func (o *VectorStats) SetLruEvictions(v int32)`

SetLruEvictions sets LruEvictions field to given value.


### GetShardDetails

`func (o *VectorStats) GetShardDetails() string`

GetShardDetails returns the ShardDetails field if non-nil, zero value otherwise.

### GetShardDetailsOk

`func (o *VectorStats) GetShardDetailsOk() (*string, bool)`

GetShardDetailsOk returns a tuple with the ShardDetails field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetShardDetails

`func (o *VectorStats) SetShardDetails(v string)`

SetShardDetails sets ShardDetails field to given value.


### SetShardDetailsNil

`func (o *VectorStats) SetShardDetailsNil(b bool)`

 SetShardDetailsNil sets the value for ShardDetails to be an explicit nil

### UnsetShardDetails
`func (o *VectorStats) UnsetShardDetails()`

UnsetShardDetails ensures that no value is present for ShardDetails, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


