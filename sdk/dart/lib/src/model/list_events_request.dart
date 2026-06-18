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
/// * [timeMinCommaOmitempty] 
/// * [timeMaxCommaOmitempty] 
/// * [maxResultsCommaOmitempty] 
@BuiltValue()
abstract class ListEventsRequest implements Built<ListEventsRequest, ListEventsRequestBuilder> {
  @BuiltValueField(wireName: r'time_min,omitempty')
  String? get timeMinCommaOmitempty;

  @BuiltValueField(wireName: r'time_max,omitempty')
  String? get timeMaxCommaOmitempty;

  @BuiltValueField(wireName: r'max_results,omitempty')
  int? get maxResultsCommaOmitempty;

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
    if (object.timeMinCommaOmitempty != null) {
      yield r'time_min,omitempty';
      yield serializers.serialize(
        object.timeMinCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.timeMaxCommaOmitempty != null) {
      yield r'time_max,omitempty';
      yield serializers.serialize(
        object.timeMaxCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.maxResultsCommaOmitempty != null) {
      yield r'max_results,omitempty';
      yield serializers.serialize(
        object.maxResultsCommaOmitempty,
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
        case r'time_min,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.timeMinCommaOmitempty = valueDes;
          break;
        case r'time_max,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.timeMaxCommaOmitempty = valueDes;
          break;
        case r'max_results,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.maxResultsCommaOmitempty = valueDes;
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

