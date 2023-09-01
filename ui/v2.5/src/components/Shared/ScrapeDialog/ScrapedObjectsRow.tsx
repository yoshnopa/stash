import React, { useMemo } from "react";
import * as GQL from "src/core/generated-graphql";
import {
  MovieSelect,
  TagSelect,
  StudioSelect,
} from "src/components/Shared/Select";
import {
  ScrapeDialogRow,
  IHasName,
} from "src/components/Shared/ScrapeDialog/ScrapeDialog";
import { PerformerSelect } from "src/components/Performers/PerformerSelect";
import { ScrapeResult } from "src/components/Shared/ScrapeDialog/scrapeResult";

interface IScrapedStudioRow {
  title: string;
  result: ScrapeResult<string>;
  onChange: (value: ScrapeResult<string>) => void;
  newStudio?: GQL.ScrapedStudio;
  onCreateNew?: (value: GQL.ScrapedStudio) => void;
}

export const ScrapedStudioRow: React.FC<IScrapedStudioRow> = ({
  title,
  result,
  onChange,
  newStudio,
  onCreateNew,
}) => {
  function renderScrapedStudio(
    scrapeResult: ScrapeResult<string>,
    isNew?: boolean,
    onChangeFn?: (value: string) => void
  ) {
    const resultValue = isNew
      ? scrapeResult.newValue
      : scrapeResult.originalValue;
    const value = resultValue ? [resultValue] : [];

    return (
      <StudioSelect
        className="form-control react-select"
        isDisabled={!isNew}
        onSelect={(items) => {
          if (onChangeFn) {
            onChangeFn(items[0]?.id);
          }
        }}
        ids={value}
      />
    );
  }

  return (
    <ScrapeDialogRow
      title={title}
      result={result}
      renderOriginalField={() => renderScrapedStudio(result)}
      renderNewField={() =>
        renderScrapedStudio(result, true, (value) =>
          onChange(result.cloneWithValue(value))
        )
      }
      onChange={onChange}
      newValues={newStudio ? [newStudio] : undefined}
      onCreateNew={() => {
        if (onCreateNew && newStudio) onCreateNew(newStudio);
      }}
    />
  );
};

interface IScrapedObjectsRow<T, R> {
  title: string;
  result: ScrapeResult<R[]>;
  onChange: (value: ScrapeResult<R[]>) => void;
  newObjects?: T[];
  onCreateNew?: (value: T) => void;
  renderObjects: (
    result: ScrapeResult<R[]>,
    isNew?: boolean,
    onChange?: (value: R[]) => void
  ) => JSX.Element;
}

export const ScrapedObjectsRow = <T extends IHasName, R>(
  props: IScrapedObjectsRow<T, R>
) => {
  const { title, result, onChange, newObjects, onCreateNew, renderObjects } =
    props;

  return (
    <ScrapeDialogRow
      title={title}
      result={result}
      renderOriginalField={() => renderObjects(result)}
      renderNewField={() =>
        renderObjects(result, true, (value) =>
          onChange(result.cloneWithValue(value))
        )
      }
      onChange={onChange}
      newValues={newObjects}
      onCreateNew={(i) => {
        if (onCreateNew) onCreateNew(newObjects![i]);
      }}
    />
  );
};

type IScrapedObjectRowImpl<T, R> = Omit<
  IScrapedObjectsRow<T, R>,
  "renderObjects"
>;

export const ScrapedPerformersRow: React.FC<
  IScrapedObjectRowImpl<GQL.ScrapedPerformer, GQL.ScrapedPerformer>
> = ({ title, result, onChange, newObjects, onCreateNew }) => {
  const performersCopy = useMemo(() => {
    return (
      newObjects?.map((p) => {
        const name: string = p.name ?? "";
        return { ...p, name };
      }) ?? []
    );
  }, [newObjects]);

  function renderScrapedPerformers(
    scrapeResult: ScrapeResult<GQL.ScrapedPerformer[]>,
    isNew?: boolean,
    onChangeFn?: (value: GQL.ScrapedPerformer[]) => void
  ) {
    const resultValue = isNew
      ? scrapeResult.newValue
      : scrapeResult.originalValue;
    const value = resultValue ?? [];

    const selectValue = value.map((p) => {
      const alias_list: string[] = [];
      return {
        id: p.stored_id ?? "",
        name: p.name ?? "",
        alias_list,
      };
    });

    return (
      <PerformerSelect
        isMulti
        className="form-control react-select"
        isDisabled={!isNew}
        onSelect={(items) => {
          if (onChangeFn) {
            onChangeFn(items);
          }
        }}
        values={selectValue}
      />
    );
  }

  type PerformerType = GQL.ScrapedPerformer & {
    name: string;
  };

  return (
    <ScrapedObjectsRow<PerformerType, GQL.ScrapedPerformer>
      title={title}
      result={result}
      renderObjects={renderScrapedPerformers}
      onChange={onChange}
      newObjects={performersCopy}
      onCreateNew={onCreateNew}
    />
  );
};

export const ScrapedMoviesRow: React.FC<
  IScrapedObjectRowImpl<GQL.ScrapedMovie, string>
> = ({ title, result, onChange, newObjects, onCreateNew }) => {
  const moviesCopy = useMemo(() => {
    return (
      newObjects?.map((p) => {
        const name: string = p.name ?? "";
        return { ...p, name };
      }) ?? []
    );
  }, [newObjects]);

  type MovieType = GQL.ScrapedMovie & {
    name: string;
  };

  function renderScrapedMovies(
    scrapeResult: ScrapeResult<string[]>,
    isNew?: boolean,
    onChangeFn?: (value: string[]) => void
  ) {
    const resultValue = isNew
      ? scrapeResult.newValue
      : scrapeResult.originalValue;
    const value = resultValue ?? [];

    return (
      <MovieSelect
        isMulti
        className="form-control react-select"
        isDisabled={!isNew}
        onSelect={(items) => {
          if (onChangeFn) {
            onChangeFn(items.map((i) => i.id));
          }
        }}
        ids={value}
      />
    );
  }

  return (
    <ScrapedObjectsRow<MovieType, string>
      title={title}
      result={result}
      renderObjects={renderScrapedMovies}
      onChange={onChange}
      newObjects={moviesCopy}
      onCreateNew={onCreateNew}
    />
  );
};

export const ScrapedTagsRow: React.FC<
  IScrapedObjectRowImpl<GQL.ScrapedTag, string>
> = ({ title, result, onChange, newObjects, onCreateNew }) => {
  function renderScrapedTags(
    scrapeResult: ScrapeResult<string[]>,
    isNew?: boolean,
    onChangeFn?: (value: string[]) => void
  ) {
    const resultValue = isNew
      ? scrapeResult.newValue
      : scrapeResult.originalValue;
    const value = resultValue ?? [];

    return (
      <TagSelect
        isMulti
        className="form-control react-select"
        isDisabled={!isNew}
        onSelect={(items) => {
          if (onChangeFn) {
            onChangeFn(items.map((i) => i.id));
          }
        }}
        ids={value}
      />
    );
  }

  return (
    <ScrapedObjectsRow<GQL.ScrapedTag, string>
      title={title}
      result={result}
      renderObjects={renderScrapedTags}
      onChange={onChange}
      newObjects={newObjects}
      onCreateNew={onCreateNew}
    />
  );
};
