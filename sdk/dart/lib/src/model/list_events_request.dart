//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'list_events_request.g.dart';

/// ListEventsRequest
///
/// Properties:
/// * [timeMin] 
/// * [timeMax] 
/// * [maxResults] 
@BuiltValue()
abstract class ListEventsRequest implements Built<ListEventsRequest, ListEventsRequestBuilder> {
  @BuiltValueField(wireName: r'time_min')
  String? get timeMin;

  @BuiltValueField(wireName: r'time_max')
  String? get timeMax;

  @BuiltValueField(wireName: r'max_results')
  int? get maxResults;

  ListEventsRequest._();

  factory ListEventsRequest([void updates(ListEventsRequestBuilder b)]) = _$ListEventsRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ListEventsRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ListEventsRequest> get serializer => _$ListEventsRequestSerializer();
}

class _$ListEventsRequestSerializer implements PrimitiveSerializer<ListEventsRequest> {
  @override
  final Iterable<Type> types = const [ListEventsRequest, _$ListEventsRequest];

  @override
  final String wireName = r'ListEventsRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ListEventsRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.timeMin != null) {
      yield r'time_min';
      yield serializers.serialize(
        object.timeMin,
        specifiedType: const FullType(String),
      );
    }
    if (object.timeMax != null) {
      yield r'time_max';
      yield serializers.serialize(
        object.timeMax,
        specifiedType: const FullType(String),
      );
    }
    if (object.maxResults != null) {
      yield r'max_results';
      yield serializers.serialize(
        object.maxResults,
        specifiedType: const FullType(int),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ListEventsRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ListEventsRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'time_min':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.timeMin = valueDes;
          break;
        case r'time_max':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.timeMax = valueDes;
          break;
        case r'max_results':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxResults = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ListEventsRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ListEventsRequestBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

