/* eslint-disable consistent-return */
/* eslint @typescript-eslint/no-unused-vars: ["error", { "argsIgnorePattern": "^_" }] */

import { IntlShape } from "react-intl";
import {
  CriterionModifier,
  HierarchicalMultiCriterionInput,
  IntCriterionInput,
  MultiCriterionInput,
  PHashDuplicationCriterionInput,
  DateCriterionInput,
  TimestampCriterionInput,
} from "src/core/generated-graphql";
import DurationUtils from "src/utils/duration";
import {
  CriterionType,
  IHierarchicalLabelValue,
  ILabeledId,
  INumberValue,
  IOptionType,
  IStashIDValue,
  IDateValue,
  ITimestampValue,
  ILabeledValueListValue,
  IPhashDistanceValue,
} from "../types";

export type Option = string | number | IOptionType;
export type CriterionValue =
  | string
  | string[]
  | ILabeledId[]
  | IHierarchicalLabelValue
  | ILabeledValueListValue
  | INumberValue
  | IStashIDValue
  | IDateValue
  | ITimestampValue
  | IPhashDistanceValue;

export interface IEncodedCriterion<T extends CriterionValue> {
  modifier: CriterionModifier;
  value: T | undefined;
}

const modifierMessageIDs = {
  [CriterionModifier.Equals]: "criterion_modifier.equals",
  [CriterionModifier.NotEquals]: "criterion_modifier.not_equals",
  [CriterionModifier.GreaterThan]: "criterion_modifier.greater_than",
  [CriterionModifier.LessThan]: "criterion_modifier.less_than",
  [CriterionModifier.IsNull]: "criterion_modifier.is_null",
  [CriterionModifier.NotNull]: "criterion_modifier.not_null",
  [CriterionModifier.Includes]: "criterion_modifier.includes",
  [CriterionModifier.IncludesAll]: "criterion_modifier.includes_all",
  [CriterionModifier.Excludes]: "criterion_modifier.excludes",
  [CriterionModifier.MatchesRegex]: "criterion_modifier.matches_regex",
  [CriterionModifier.NotMatchesRegex]: "criterion_modifier.not_matches_regex",
  [CriterionModifier.Between]: "criterion_modifier.between",
  [CriterionModifier.NotBetween]: "criterion_modifier.not_between",
};

// V = criterion value type
export abstract class Criterion<V extends CriterionValue> {
  public criterionOption: CriterionOption;

  protected _modifier!: CriterionModifier;
  public get modifier(): CriterionModifier {
    return this._modifier;
  }
  public set modifier(value: CriterionModifier) {
    this._modifier = value;
  }

  protected _value!: V;
  public get value(): V {
    return this._value;
  }
  public set value(newValue: V) {
    this._value = newValue;
  }

  public isValid(): boolean {
    return true;
  }

  protected abstract getLabelValue(intl: IntlShape): string;

  constructor(type: CriterionOption, value: V) {
    this.criterionOption = type;
    this.modifier = type.defaultModifier;
    this.value = value;
  }

  public static getModifierLabel(intl: IntlShape, modifier: CriterionModifier) {
    const modifierMessageID = modifierMessageIDs[modifier];

    return modifierMessageID
      ? intl.formatMessage({ id: modifierMessageID })
      : "";
  }

  public getLabel(intl: IntlShape): string {
    const modifierString = Criterion.getModifierLabel(intl, this.modifier);
    let valueString = "";

    if (
      this.modifier !== CriterionModifier.IsNull &&
      this.modifier !== CriterionModifier.NotNull
    ) {
      valueString = this.getLabelValue(intl);
    }

    return intl.formatMessage(
      { id: "criterion_modifier.format_string" },
      {
        criterion: intl.formatMessage({ id: this.criterionOption.messageID }),
        modifierString,
        valueString,
      }
    );
  }

  public getId(): string {
    return `${this.criterionOption.type}-${this.modifier.toString()}`; // TODO add values?
  }

  public toJSON() {
    let encodedCriterion;
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      encodedCriterion = {
        type: this.criterionOption.type,
        modifier: this.modifier,
      };
    } else {
      encodedCriterion = {
        type: this.criterionOption.type,
        value: this.value,
        modifier: this.modifier,
      };
    }
    return JSON.stringify(encodedCriterion);
  }

  public setFromEncodedCriterion(encodedCriterion: IEncodedCriterion<V>) {
    if (encodedCriterion.value !== undefined) {
      this.value = encodedCriterion.value;
    }
    this.modifier = encodedCriterion.modifier;
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public apply(outputFilter: Record<string, any>) {
    // eslint-disable-next-line no-param-reassign
    outputFilter[this.criterionOption.type] = this.toCriterionInput();
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  protected toCriterionInput(): any {
    return {
      value: this.value,
      modifier: this.modifier,
    };
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  public toSavedFilter(outputFilter: Record<string, any>) {
    outputFilter[this.criterionOption.type] = {
      value: this.value,
      modifier: this.modifier,
    };
  }
}

export type InputType =
  | "number"
  | "text"
  | "performers"
  | "studios"
  | "tags"
  | "performer_tags"
  | "scene_tags"
  | "movies"
  | "galleries"
  | undefined;

interface ICriterionOptionsParams {
  messageID: string;
  type: CriterionType;
  inputType?: InputType;
  modifierOptions?: CriterionModifier[];
  defaultModifier?: CriterionModifier;
  options?: Option[];
  makeCriterion: () => Criterion<CriterionValue>;
}
export class CriterionOption {
  public readonly messageID: string;
  public readonly type: CriterionType;
  public readonly modifierOptions: CriterionModifier[];
  public readonly defaultModifier: CriterionModifier;
  public readonly options: Option[] | undefined;
  public readonly inputType: InputType;
  public readonly makeCriterionFn: (
    o: CriterionOption
  ) => Criterion<CriterionValue>;

  constructor(options: ICriterionOptionsParams) {
    this.messageID = options.messageID;
    this.type = options.type;
    this.modifierOptions = options.modifierOptions ?? [];
    this.defaultModifier = options.defaultModifier ?? CriterionModifier.Equals;
    this.options = options.options;
    this.inputType = options.inputType;
    this.makeCriterionFn = options.makeCriterion;
  }

  public makeCriterion() {
    return this.makeCriterionFn(this);
  }
}

export class StringCriterionOption extends CriterionOption {
  constructor(messageID: string, type: CriterionType, options?: Option[]) {
    super({
      messageID,
      type,
      modifierOptions: [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.Includes,
        CriterionModifier.Excludes,
        CriterionModifier.IsNull,
        CriterionModifier.NotNull,
        CriterionModifier.MatchesRegex,
        CriterionModifier.NotMatchesRegex,
      ],
      defaultModifier: CriterionModifier.Equals,
      options,
      inputType: "text",
      makeCriterion: () => new StringCriterion(this),
    });
  }
}

export function createStringCriterionOption(
  type: CriterionType,
  messageID?: string
) {
  return new StringCriterionOption(messageID ?? type, type);
}

export class StringCriterion extends Criterion<string> {
  constructor(type: CriterionOption) {
    super(type, "");
  }

  protected getLabelValue(_intl: IntlShape) {
    return this.value;
  }

  public isValid(): boolean {
    return (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull ||
      this.value.length > 0
    );
  }
}

export class MultiStringCriterion extends Criterion<string[]> {
  constructor(type: CriterionOption) {
    super(type, []);
  }

  protected getLabelValue(_intl: IntlShape) {
    return this.value.join(", ");
  }

  public isValid(): boolean {
    return (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull ||
      this.value.length > 0
    );
  }
}

export class MandatoryStringCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType, options?: Option[]) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.Includes,
        CriterionModifier.Excludes,
        CriterionModifier.MatchesRegex,
        CriterionModifier.NotMatchesRegex,
      ],
      defaultModifier: CriterionModifier.Equals,
      options,
      inputType: "text",
      makeCriterion: () => new StringCriterion(this),
    });
  }
}

export function createMandatoryStringCriterionOption(
  value: CriterionType,
  messageID?: string
) {
  return new MandatoryStringCriterionOption(messageID ?? value, value);
}

export class PathCriterionOption extends StringCriterionOption {}

export function createPathCriterionOption(
  type: CriterionType,
  messageID?: string
) {
  return new PathCriterionOption(messageID ?? type, type);
}

export class BooleanCriterionOption extends CriterionOption {
  constructor(
    messageID: string,
    value: CriterionType,
    makeCriterion?: () => Criterion<CriterionValue>
  ) {
    super({
      messageID,
      type: value,
      modifierOptions: [],
      defaultModifier: CriterionModifier.Equals,
      options: [true.toString(), false.toString()],
      makeCriterion: makeCriterion
        ? makeCriterion
        : () => new BooleanCriterion(this),
    });
  }
}

export class BooleanCriterion extends StringCriterion {
  protected toCriterionInput(): boolean {
    return this.value === "true";
  }

  public isValid() {
    return this.value === "true" || this.value === "false";
  }
}

export function createBooleanCriterionOption(
  value: CriterionType,
  messageID?: string
) {
  return new BooleanCriterionOption(messageID ?? value, value);
}

export class NumberCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType, options?: Option[]) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.GreaterThan,
        CriterionModifier.LessThan,
        CriterionModifier.IsNull,
        CriterionModifier.NotNull,
        CriterionModifier.Between,
        CriterionModifier.NotBetween,
      ],
      defaultModifier: CriterionModifier.Equals,
      options,
      inputType: "number",
      makeCriterion: () => new NumberCriterion(this),
    });
  }
}

export class NullNumberCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.GreaterThan,
        CriterionModifier.LessThan,
        CriterionModifier.Between,
        CriterionModifier.NotBetween,
        CriterionModifier.IsNull,
        CriterionModifier.NotNull,
      ],
      defaultModifier: CriterionModifier.Equals,
      inputType: "number",
      makeCriterion: () => new NumberCriterion(this),
    });
  }
}

export function createNumberCriterionOption(value: CriterionType) {
  return new NumberCriterionOption(value, value);
}

export function createNullNumberCriterionOption(value: CriterionType) {
  return new NullNumberCriterionOption(value, value);
}

export class NumberCriterion extends Criterion<INumberValue> {
  public get value(): INumberValue {
    return this._value;
  }
  public set value(newValue: number | INumberValue) {
    // backwards compatibility - if this.value is a number, use that
    if (typeof newValue !== "object") {
      this._value = {
        value: newValue,
        value2: undefined,
      };
    } else {
      this._value = newValue;
    }
  }

  protected toCriterionInput(): IntCriterionInput {
    return {
      modifier: this.modifier,
      value: this.value?.value ?? 0,
      value2: this.value?.value2,
    };
  }

  protected getLabelValue(_intl: IntlShape) {
    const { value, value2 } = this.value;
    if (
      this.modifier === CriterionModifier.Between ||
      this.modifier === CriterionModifier.NotBetween
    ) {
      return `${value}, ${value2 ?? 0}`;
    } else {
      return `${value}`;
    }
  }

  public isValid(): boolean {
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }

    const { value, value2 } = this.value;
    if (value === undefined) {
      return false;
    }

    if (
      value2 === undefined &&
      (this.modifier === CriterionModifier.Between ||
        this.modifier === CriterionModifier.NotBetween)
    ) {
      return false;
    }

    return true;
  }

  constructor(type: CriterionOption) {
    super(type, { value: undefined, value2: undefined });
  }
}

export class ILabeledIdCriterionOption extends CriterionOption {
  constructor(
    messageID: string,
    value: CriterionType,
    includeAll: boolean,
    inputType: InputType
  ) {
    const modifierOptions = [
      CriterionModifier.Includes,
      CriterionModifier.Excludes,
      CriterionModifier.IsNull,
      CriterionModifier.NotNull,
    ];

    let defaultModifier = CriterionModifier.Includes;
    if (includeAll) {
      modifierOptions.unshift(CriterionModifier.IncludesAll);
      defaultModifier = CriterionModifier.IncludesAll;
    }

    super({
      messageID,
      type: value,
      modifierOptions,
      defaultModifier,
      makeCriterion: () => new ILabeledIdCriterion(this),
      inputType,
    });
  }
}

export class ILabeledIdCriterion extends Criterion<ILabeledId[]> {
  protected getLabelValue(_intl: IntlShape): string {
    return this.value.map((v) => v.label).join(", ");
  }

  protected toCriterionInput(): MultiCriterionInput {
    return {
      value: this.value.map((v) => v.id),
      modifier: this.modifier,
    };
  }

  public isValid(): boolean {
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }

    return this.value.length > 0;
  }

  constructor(type: CriterionOption) {
    super(type, []);
  }
}

export class IHierarchicalLabeledIdCriterion extends Criterion<IHierarchicalLabelValue> {
  constructor(type: CriterionOption) {
    const value: IHierarchicalLabelValue = {
      items: [],
      excluded: [],
      depth: 0,
    };

    super(type, value);
  }

  override get modifier(): CriterionModifier {
    return this._modifier;
  }
  override set modifier(value: CriterionModifier) {
    this._modifier = value;

    // excluded only makes sense for includes and includes all
    // so reset it for other modifiers
    if (
      value !== CriterionModifier.Includes &&
      value !== CriterionModifier.IncludesAll
    ) {
      this.value.excluded = [];
    }
  }

  public setFromEncodedCriterion(
    encodedCriterion: IEncodedCriterion<IHierarchicalLabelValue>
  ) {
    const { modifier, value } = encodedCriterion;

    if (value !== undefined) {
      this.value = {
        items: value.items || [],
        excluded: value.excluded || [],
        depth: value.depth || 0,
      };
    }

    // if the previous modifier was excludes, replace it with the equivalent includes criterion
    // this is what is done on the backend
    // only replace if excludes is not a valid modifierOption
    if (
      modifier === CriterionModifier.Excludes &&
      this.criterionOption.modifierOptions.find(
        (m) => m === CriterionModifier.Excludes
      ) === undefined
    ) {
      this.modifier = CriterionModifier.Includes;
      this.value.excluded = [...this.value.excluded, ...this.value.items];
      this.value.items = [];
    } else {
      this.modifier = modifier;
    }
  }

  protected getLabelValue(_intl: IntlShape): string {
    const labels = (this.value.items ?? []).map((v) => v.label).join(", ");

    if (this.value.depth === 0) {
      return labels;
    }

    return `${labels} (+${this.value.depth > 0 ? this.value.depth : "all"})`;
  }

  protected toCriterionInput(): HierarchicalMultiCriterionInput {
    let excludes: string[] = [];

    // if modifier is equals, depth must be 0
    const depth =
      this.modifier === CriterionModifier.Equals ? 0 : this.value.depth;

    if (this.value.excluded) {
      excludes = this.value.excluded.map((v) => v.id);
    }
    return {
      value: this.value.items.map((v) => v.id),
      excludes: excludes,
      modifier: this.modifier,
      depth,
    };
  }

  public isValid(): boolean {
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }

    return (
      this.value.items.length > 0 ||
      (this.value.excluded && this.value.excluded.length > 0)
    );
  }

  public getLabel(intl: IntlShape): string {
    let id = "criterion_modifier.format_string";
    let modifierString = Criterion.getModifierLabel(intl, this.modifier);
    let valueString = "";
    let excludedString = "";

    if (
      this.modifier !== CriterionModifier.IsNull &&
      this.modifier !== CriterionModifier.NotNull
    ) {
      valueString = this.value.items.map((v) => v.label).join(", ");

      if (this.value.excluded && this.value.excluded.length > 0) {
        if (this.value.items.length === 0) {
          modifierString = Criterion.getModifierLabel(
            intl,
            CriterionModifier.Excludes
          );
          valueString = this.value.excluded.map((v) => v.label).join(", ");
        } else {
          id = "criterion_modifier.format_string_excludes";
          excludedString = this.value.excluded.map((v) => v.label).join(", ");
        }
      }

      if (this.value.depth !== 0) {
        id += "_depth";
      }
    }

    return intl.formatMessage(
      { id },
      {
        criterion: intl.formatMessage({ id: this.criterionOption.messageID }),
        modifierString,
        valueString,
        excludedString,
        depth: this.value.depth,
      }
    );
  }
}

export class MandatoryNumberCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.GreaterThan,
        CriterionModifier.LessThan,
        CriterionModifier.Between,
        CriterionModifier.NotBetween,
      ],
      defaultModifier: CriterionModifier.Equals,
      inputType: "number",
      makeCriterion: () => new NumberCriterion(this),
    });
  }
}

export function createMandatoryNumberCriterionOption(
  value: CriterionType,
  messageID?: string
) {
  return new MandatoryNumberCriterionOption(messageID ?? value, value);
}

export class DurationCriterion extends Criterion<INumberValue> {
  constructor(type: CriterionOption) {
    super(type, { value: undefined, value2: undefined });
  }

  protected toCriterionInput(): IntCriterionInput {
    return {
      modifier: this.modifier,
      value: this.value?.value ?? 0,
      value2: this.value?.value2,
    };
  }

  protected getLabelValue(_intl: IntlShape) {
    return this.modifier === CriterionModifier.Between ||
      this.modifier === CriterionModifier.NotBetween
      ? `${DurationUtils.secondsToString(
          this.value.value ?? 0
        )} ${DurationUtils.secondsToString(this.value.value2 ?? 0)}`
      : this.modifier === CriterionModifier.GreaterThan ||
        this.modifier === CriterionModifier.LessThan ||
        this.modifier === CriterionModifier.Equals ||
        this.modifier === CriterionModifier.NotEquals
      ? DurationUtils.secondsToString(this.value.value ?? 0)
      : "?";
  }

  public isValid(): boolean {
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }

    const { value, value2 } = this.value;
    if (value === undefined) {
      return false;
    }

    if (
      value2 === undefined &&
      (this.modifier === CriterionModifier.Between ||
        this.modifier === CriterionModifier.NotBetween)
    ) {
      return false;
    }

    return true;
  }
}

export class PhashDuplicateCriterion extends StringCriterion {
  protected toCriterionInput(): PHashDuplicationCriterionInput {
    return {
      duplicated: this.value === "true",
    };
  }
}

export class DateCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType, options?: Option[]) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.Equals,
        CriterionModifier.NotEquals,
        CriterionModifier.GreaterThan,
        CriterionModifier.LessThan,
        CriterionModifier.IsNull,
        CriterionModifier.NotNull,
        CriterionModifier.Between,
        CriterionModifier.NotBetween,
      ],
      defaultModifier: CriterionModifier.Equals,
      options,
      inputType: "text",
      makeCriterion: () => new DateCriterion(this),
    });
  }
}

export function createDateCriterionOption(value: CriterionType) {
  return new DateCriterionOption(value, value);
}

export class DateCriterion extends Criterion<IDateValue> {
  public encodeValue() {
    return {
      value: this.value.value,
      value2: this.value.value2,
    };
  }

  protected toCriterionInput(): DateCriterionInput {
    return {
      modifier: this.modifier,
      value: this.value?.value,
      value2: this.value?.value2,
    };
  }

  protected getLabelValue() {
    const { value } = this.value;
    return this.modifier === CriterionModifier.Between ||
      this.modifier === CriterionModifier.NotBetween
      ? `${value}, ${this.value.value2}`
      : `${value}`;
  }

  public isValid(): boolean {
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }

    const { value, value2 } = this.value;
    if (!value) {
      return false;
    }

    if (
      !value2 &&
      (this.modifier === CriterionModifier.Between ||
        this.modifier === CriterionModifier.NotBetween)
    ) {
      return false;
    }

    return true;
  }

  constructor(type: CriterionOption) {
    super(type, { value: "", value2: undefined });
  }
}

export class TimestampCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType, options?: Option[]) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.GreaterThan,
        CriterionModifier.LessThan,
        CriterionModifier.IsNull,
        CriterionModifier.NotNull,
        CriterionModifier.Between,
        CriterionModifier.NotBetween,
      ],
      defaultModifier: CriterionModifier.GreaterThan,
      options,
      inputType: "text",
      makeCriterion: () => new TimestampCriterion(this),
    });
  }
}

export function createTimestampCriterionOption(value: CriterionType) {
  return new TimestampCriterionOption(value, value);
}

export class TimestampCriterion extends Criterion<ITimestampValue> {
  public encodeValue() {
    return {
      value: this.value?.value,
      value2: this.value?.value2,
    };
  }

  protected toCriterionInput(): TimestampCriterionInput {
    return {
      modifier: this.modifier,
      value: this.transformValueToInput(this.value.value),
      value2: this.value.value2
        ? this.transformValueToInput(this.value.value2)
        : null,
    };
  }

  protected getLabelValue() {
    const { value } = this.value;
    return this.modifier === CriterionModifier.Between ||
      this.modifier === CriterionModifier.NotBetween
      ? `${value}, ${this.value.value2}`
      : `${value}`;
  }

  private transformValueToInput(value: string): string {
    value = value.trim();
    if (/^\d{4}-\d{2}-\d{2}(( |T)\d{2}:\d{2})?$/.test(value)) {
      return value.replace(" ", "T");
    }

    return "";
  }

  public isValid(): boolean {
    if (
      this.modifier === CriterionModifier.IsNull ||
      this.modifier === CriterionModifier.NotNull
    ) {
      return true;
    }

    const { value, value2 } = this.value;
    if (!value) {
      return false;
    }

    if (
      !value2 &&
      (this.modifier === CriterionModifier.Between ||
        this.modifier === CriterionModifier.NotBetween)
    ) {
      return false;
    }

    return true;
  }

  constructor(type: CriterionOption) {
    super(type, { value: "", value2: undefined });
  }
}

export class MandatoryTimestampCriterionOption extends CriterionOption {
  constructor(messageID: string, value: CriterionType, options?: Option[]) {
    super({
      messageID,
      type: value,
      modifierOptions: [
        CriterionModifier.GreaterThan,
        CriterionModifier.LessThan,
        CriterionModifier.Between,
        CriterionModifier.NotBetween,
      ],
      defaultModifier: CriterionModifier.GreaterThan,
      options,
      inputType: "text",
      makeCriterion: () => new TimestampCriterion(this),
    });
  }
}

export function createMandatoryTimestampCriterionOption(value: CriterionType) {
  return new MandatoryTimestampCriterionOption(value, value);
}
