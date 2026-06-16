# ListEventsResponse

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Events** | **[]string** |  | 
**Count** | **int32** |  | 

## Methods

### NewListEventsResponse

`func NewListEventsResponse(events []string, count int32, ) *ListEventsResponse`

NewListEventsResponse instantiates a new ListEventsResponse object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewListEventsResponseWithDefaults

`func NewListEventsResponseWithDefaults() *ListEventsResponse`

NewListEventsResponseWithDefaults instantiates a new ListEventsResponse object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetEvents

`func (o *ListEventsResponse) GetEvents() []string`

GetEvents returns the Events field if non-nil, zero value otherwise.

### GetEventsOk

`func (o *ListEventsResponse) GetEventsOk() (*[]string, bool)`

GetEventsOk returns a tuple with the Events field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEvents

`func (o *ListEventsResponse) SetEvents(v []string)`

SetEvents sets Events field to given value.


### SetEventsNil

`func (o *ListEventsResponse) SetEventsNil(b bool)`

 SetEventsNil sets the value for Events to be an explicit nil

### UnsetEvents
`func (o *ListEventsResponse) UnsetEvents()`

UnsetEvents ensures that no value is present for Events, not even an explicit nil
### GetCount

`func (o *ListEventsResponse) GetCount() int32`

GetCount returns the Count field if non-nil, zero value otherwise.

### GetCountOk

`func (o *ListEventsResponse) GetCountOk() (*int32, bool)`

GetCountOk returns a tuple with the Count field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetCount

`func (o *ListEventsResponse) SetCount(v int32)`

SetCount sets Count field to given value.



[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


