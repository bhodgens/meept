# PaginatedResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Items** | **[]string** |  | 
**Total** | **int32** |  | 
**HasMore** | **bool** |  | 
**NextOffsetomitempty** | Pointer to **int32** |  | [optional] 

## Methods

### NewPaginatedResponse

`func NewPaginatedResponse(items []string, total int32, hasMore bool, ) *PaginatedResponse`

NewPaginatedResponse instantiates a new PaginatedResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewPaginatedResponseWithDefaults

`func NewPaginatedResponseWithDefaults() *PaginatedResponse`

NewPaginatedResponseWithDefaults instantiates a new PaginatedResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetItems

`func (o *PaginatedResponse) GetItems() []string`

GetItems returns the Items field if non-nil, zero value otherwise.

### GetItemsOk

`func (o *PaginatedResponse) GetItemsOk() (*[]string, bool)`

GetItemsOk returns a tuple with the Items field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetItems

`func (o *PaginatedResponse) SetItems(v []string)`

SetItems sets Items field to given value.


### SetItemsNil

`func (o *PaginatedResponse) SetItemsNil(b bool)`

 SetItemsNil sets the value for Items to be an explicit nil

### UnsetItems
`func (o *PaginatedResponse) UnsetItems()`

UnsetItems ensures that no value is present for Items, not even an explicit nil
### GetTotal

`func (o *PaginatedResponse) GetTotal() int32`

GetTotal returns the Total field if non-nil, zero value otherwise.

### GetTotalOk

`func (o *PaginatedResponse) GetTotalOk() (*int32, bool)`

GetTotalOk returns a tuple with the Total field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetTotal

`func (o *PaginatedResponse) SetTotal(v int32)`

SetTotal sets Total field to given value.


### GetHasMore

`func (o *PaginatedResponse) GetHasMore() bool`

GetHasMore returns the HasMore field if non-nil, zero value otherwise.

### GetHasMoreOk

`func (o *PaginatedResponse) GetHasMoreOk() (*bool, bool)`

GetHasMoreOk returns a tuple with the HasMore field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetHasMore

`func (o *PaginatedResponse) SetHasMore(v bool)`

SetHasMore sets HasMore field to given value.


### GetNextOffsetomitempty

`func (o *PaginatedResponse) GetNextOffsetomitempty() int32`

GetNextOffsetomitempty returns the NextOffsetomitempty field if non-nil, zero value otherwise.

### GetNextOffsetomitemptyOk

`func (o *PaginatedResponse) GetNextOffsetomitemptyOk() (*int32, bool)`

GetNextOffsetomitemptyOk returns a tuple with the NextOffsetomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetNextOffsetomitempty

`func (o *PaginatedResponse) SetNextOffsetomitempty(v int32)`

SetNextOffsetomitempty sets NextOffsetomitempty field to given value.

### HasNextOffsetomitempty

`func (o *PaginatedResponse) HasNextOffsetomitempty() bool`

HasNextOffsetomitempty returns a boolean if a field has been set.


[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


