// NOTE: add new enum values to the end, to ensure existing data

// is not impacted
export enum DisplayMode {
  Grid,
  List,
  Wall,
  Tagger,
}

export interface ILabeledId {
  id: string;
  label: string;
}

export interface ILabeledValue {
  label: string;
  value: string;
}

export interface ILabeledValueListValue {
  items: ILabeledId[];
  excluded: ILabeledId[];
}

export interface IHierarchicalLabelValue {
  items: ILabeledId[];
  excluded: ILabeledId[];
  depth: number;
}

export interface INumberValue {
  value: number | undefined;
  value2: number | undefined;
}

export interface IPHashDuplicationValue {
  duplicated: boolean;
  distance?: number; // currently not implemented
}

export interface IStashIDValue {
  endpoint: string;
  stashID: string;
}

export interface IDateValue {
  value: string;
  value2: string | undefined;
}

export interface ITimestampValue {
  value: string;
  value2: string | undefined;
}

export interface IPhashDistanceValue {
  value: string;
  distance?: number;
}

export function criterionIsHierarchicalLabelValue(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  value: any
): value is IHierarchicalLabelValue {
  return typeof value === "object" && "items" in value && "depth" in value;
}

export function criterionIsNumberValue(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  value: any
): value is INumberValue {
  return typeof value === "object" && "value" in value && "value2" in value;
}

export function criterionIsStashIDValue(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  value: any
): value is IStashIDValue {
  return typeof value === "object" && "endpoint" in value && "stashID" in value;
}

export function criterionIsDateValue(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  value: any
): value is IDateValue {
  return typeof value === "object" && "value" in value && "value2" in value;
}

export function criterionIsTimestampValue(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  value: any
): value is ITimestampValue {
  return typeof value === "object" && "value" in value && "value2" in value;
}

export interface IOptionType {
  id: string;
  name?: string;
  image_path?: string;
}

export type CriterionType =
  | "none"
  | "path"
  | "rating"
  | "rating100"
  | "organized"
  | "o_counter"
  | "resolution"
  | "average_resolution"
  | "video_codec"
  | "audio_codec"
  | "duration"
  | "filter_favorites"
  | "has_markers"
  | "is_missing"
  | "tags"
  | "scene_tags"
  | "performer_tags"
  | "tag_count"
  | "performers"
  | "studios"
  | "movies"
  | "galleries"
  | "birth_year"
  | "age"
  | "ethnicity"
  | "country"
  | "hair_color"
  | "eye_color"
  | "height"
  | "height_cm"
  | "weight"
  | "measurements"
  | "fake_tits"
  | "penis_length"
  | "circumcised"
  | "career_length"
  | "tattoos"
  | "piercings"
  | "aliases"
  | "gender"
  | "parents"
  | "children"
  | "scene_count"
  | "marker_count"
  | "image_count"
  | "gallery_count"
  | "performer_count"
  | "death_year"
  | "url"
  | "stash_id"
  | "interactive"
  | "interactive_speed"
  | "captions"
  | "resume_time"
  | "play_count"
  | "play_duration"
  | "name"
  | "details"
  | "title"
  | "oshash"
  | "checksum"
  | "phash_distance"
  | "director"
  | "synopsis"
  | "parent_count"
  | "child_count"
  | "performer_favorite"
  | "performer_age"
  | "duplicated"
  | "ignore_auto_tag"
  | "file_count"
  | "stash_id_endpoint"
  | "date"
  | "created_at"
  | "updated_at"
  | "birthdate"
  | "death_date"
  | "scene_date"
  | "scene_created_at"
  | "scene_updated_at"
  | "description"
  | "code"
  | "disambiguation"
  | "has_chapters";
