//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'update_event_request.g.dart';

/// UpdateEventRequest
///
/// Properties:
/// * [id] 
/// * [summaryCommaOmitempty] 
/// * [descriptionCommaOmitempty] 
/// * [locationCommaOmitempty] 
/// * [startCommaOmitempty] 
/// * [endCommaOmitempty] 
@BuiltValue()
abstract class UpdateEventRequest implements Built<UpdateEventRequest, UpdateEventRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'summary,omitempty')
  String? get summaryCommaOmitempty;

  @BuiltValueField(wireName: r'description,omitempty')
  String? get descriptionCommaOmitempty;

  @BuiltValueField(wireName: r'location,omitempty')
  String? get locationCommaOmitempty;

  @BuiltValueField(wireName: r'start,omitempty')
  String? get startCommaOmitempty;

  @BuiltValueField(wireName: r'end,omitempty')
  String? get endCommaOmitempty;

  UpdateEventRequest._();

  factory UpdateEventRequest([void updates(UpdateEventRequestBuilder b)]) = _$UpdateEventRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(UpdateEventRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<UpdateEventRequest> get serializer => _$UpdateEventRequestSerializer();
}

class _$UpdateEventRequestSerializer implements PrimitiveSerializer<UpdateEventRequest> {
  @override
  final Iterable<Type> types = const [UpdateEventRequest, _$UpdateEventRequest];

  @override
  final String wireName = r'UpdateEventRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    UpdateEventRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    if (object.summaryCommaOmitempty != null) {
      yield r'summary,omitempty';
      yield serializers.serialize(
        object.summaryCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.descriptionCommaOmitempty != null) {
      yield r'description,omitempty';
      yield serializers.serialize(
        object.descriptionCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.locationCommaOmitempty != null) {
      yield r'location,omitempty';
      yield serializers.serialize(
        object.locationCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
    if (object.startCommaOmitempty != null) {
      yield r'start,omitempty';
      yield serializers.serialize(
        object.startCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
    if (object.endCommaOmitempty != null) {
      yield r'end,omitempty';
      yield serializers.serialize(
        object.endCommaOmitempty,
        specifiedType: const FullType.nullable(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    UpdateEventRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required UpdateEventRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'summary,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.summaryCommaOmitempty = valueDes;
          break;
        case r'description,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.descriptionCommaOmitempty = valueDes;
          break;
        case r'location,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.locationCommaOmitempty = valueDes;
          break;
        case r'start,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.startCommaOmitempty = valueDes;
          break;
        case r'end,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType.nullable(String),
          ) as String?;
          if (valueDes == null) continue;
          result.endCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  UpdateEventRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = UpdateEventRequestBuilder();
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

