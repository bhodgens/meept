# CreateEventRequest

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Summary** | **string** |  | 
**Descriptionomitempty** | Pointer to **string** |  | [optional] 
**Locationomitempty** | Pointer to **string** |  | [optional] 
**Start** | **string** |  | 
**End** | **string** |  | 
**Attendeesomitempty** | Pointer to **NullableString** |  | [optional] 

## Methods

### NewCreateEventRequest

`func NewCreateEventRequest(summary string, start string, end string, ) *CreateEventRequest`

NewCreateEventRequest instantiates a new CreateEventRequest object
This constructor will assign default values to properties that have it defined,
and makes sure properties required by API are set, but the set of arguments
will change when the set of required properties is changed

### NewCreateEventRequestWithDefaults

`func NewCreateEventRequestWithDefaults() *CreateEventRequest`

NewCreateEventRequestWithDefaults instantiates a new CreateEventRequest object
This constructor will only assign default values to properties that have it defined,
but it doesn't guarantee that properties required by API are set

### GetSummary

`func (o *CreateEventRequest) GetSummary() string`

GetSummary returns the Summary field if non-nil, zero value otherwise.

### GetSummaryOk

`func (o *CreateEventRequest) GetSummaryOk() (*string, bool)`

GetSummaryOk returns a tuple with the Summary field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetSummary

`func (o *CreateEventRequest) SetSummary(v string)`

SetSummary sets Summary field to given value.


### GetDescriptionomitempty

`func (o *CreateEventRequest) GetDescriptionomitempty() string`

GetDescriptionomitempty returns the Descriptionomitempty field if non-nil, zero value otherwise.

### GetDescriptionomitemptyOk

`func (o *CreateEventRequest) GetDescriptionomitemptyOk() (*string, bool)`

GetDescriptionomitemptyOk returns a tuple with the Descriptionomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetDescriptionomitempty

`func (o *CreateEventRequest) SetDescriptionomitempty(v string)`

SetDescriptionomitempty sets Descriptionomitempty field to given value.

### HasDescriptionomitempty

`func (o *CreateEventRequest) HasDescriptionomitempty() bool`

HasDescriptionomitempty returns a boolean if a field has been set.

### GetLocationomitempty

`func (o *CreateEventRequest) GetLocationomitempty() string`

GetLocationomitempty returns the Locationomitempty field if non-nil, zero value otherwise.

### GetLocationomitemptyOk

`func (o *CreateEventRequest) GetLocationomitemptyOk() (*string, bool)`

GetLocationomitemptyOk returns a tuple with the Locationomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetLocationomitempty

`func (o *CreateEventRequest) SetLocationomitempty(v string)`

SetLocationomitempty sets Locationomitempty field to given value.

### HasLocationomitempty

`func (o *CreateEventRequest) HasLocationomitempty() bool`

HasLocationomitempty returns a boolean if a field has been set.

### GetStart

`func (o *CreateEventRequest) GetStart() string`

GetStart returns the Start field if non-nil, zero value otherwise.

### GetStartOk

`func (o *CreateEventRequest) GetStartOk() (*string, bool)`

GetStartOk returns a tuple with the Start field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetStart

`func (o *CreateEventRequest) SetStart(v string)`

SetStart sets Start field to given value.


### GetEnd

`func (o *CreateEventRequest) GetEnd() string`

GetEnd returns the End field if non-nil, zero value otherwise.

### GetEndOk

`func (o *CreateEventRequest) GetEndOk() (*string, bool)`

GetEndOk returns a tuple with the End field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetEnd

`func (o *CreateEventRequest) SetEnd(v string)`

SetEnd sets End field to given value.


### GetAttendeesomitempty

`func (o *CreateEventRequest) GetAttendeesomitempty() string`

GetAttendeesomitempty returns the Attendeesomitempty field if non-nil, zero value otherwise.

### GetAttendeesomitemptyOk

`func (o *CreateEventRequest) GetAttendeesomitemptyOk() (*string, bool)`

GetAttendeesomitemptyOk returns a tuple with the Attendeesomitempty field if it's non-nil, zero value otherwise
and a boolean to check if the value has been set.

### SetAttendeesomitempty

`func (o *CreateEventRequest) SetAttendeesomitempty(v string)`

SetAttendeesomitempty sets Attendeesomitempty field to given value.

### HasAttendeesomitempty

`func (o *CreateEventRequest) HasAttendeesomitempty() bool`

HasAttendeesomitempty returns a boolean if a field has been set.

### SetAttendeesomitemptyNil

`func (o *CreateEventRequest) SetAttendeesomitemptyNil(b bool)`

 SetAttendeesomitemptyNil sets the value for Attendeesomitempty to be an explicit nil

### UnsetAttendeesomitempty
`func (o *CreateEventRequest) UnsetAttendeesomitempty()`

UnsetAttendeesomitempty ensures that no value is present for Attendeesomitempty, not even an explicit nil

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


