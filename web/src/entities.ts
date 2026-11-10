// Field schemas for the generic /api/v1 entities. One schema drives both the
// form and the table of an entity page (Resource.tsx); the field keys are the
// JSON names of the domain types in internal/core/domain/entities.go.

import type { Entity } from "./api";

// RefTarget is an /api/v1 collection a reference field points at. Stored
// references are canonical ids (never sourcedIds), so a ref field holds the
// referenced record's id.
export type RefTarget =
  | "persons"
  | "organizations"
  | "academic-sessions"
  | "courses"
  | "offerings"
  | "programs";

export type FieldKind = "text" | "date" | "number" | "select" | "ref";

export interface Field {
  key: string;
  label: string;
  kind: FieldKind;
  required?: boolean;
  placeholder?: string;
  // options are the choices of a "select" field.
  options?: string[];
  // ref is the collection a "ref" field points at.
  ref?: RefTarget;
  // onlyWith names the oneOf choice this field belongs to; it is hidden
  // (and cleared) while another choice is selected.
  onlyWith?: string;
}

export interface ResourceDef {
  // path is the collection under /api/v1 (writes go there), and for
  // catalog pages also the hash route.
  path: RefTarget | "enrollments" | "results";
  title: string;
  singular: string;
  fields: Field[];
  // columns are the field keys shown in the table, in order.
  columns: string[];
  // empty is the form of a new record.
  empty: Entity;
  // note explains a rule the form cannot express (shown above the form).
  note?: string;
  // oneOf: exactly one of these fields is set. The form offers a choice
  // between them and shows only the chosen field, which is then required.
  oneOf?: { label: string; keys: string[] };
}

const statusField: Field = {
  key: "status",
  label: "Status",
  kind: "select",
  options: ["active", "inactive", "tobedeleted"],
};

// sourcedIdField: optional on create (the server defaults it to the id);
// left empty on update, the stored one is kept.
const sourcedIdField: Field = {
  key: "sourcedId",
  label: "sourcedId",
  kind: "text",
  placeholder: "defaults to the id",
};

const orgField: Field = { key: "orgId", label: "Organization", kind: "ref", ref: "organizations" };

const empty: Entity = { status: "active" };

// enrollmentDef and resultDef are a student's own records: the UI edits
// them on the student page (personId fixed there), not as catalog pages.
export const enrollmentDef: ResourceDef = {
  path: "enrollments",
  title: "Enrollments",
  singular: "enrollment",
  fields: [
    { key: "personId", label: "Person", kind: "ref", ref: "persons", required: true },
    { key: "offeringId", label: "Offering", kind: "ref", ref: "offerings", required: true },
    { key: "role", label: "Role", kind: "select", options: ["student", "faculty", "staff", "guest"] },
    statusField,
    sourcedIdField,
  ],
  columns: ["personId", "offeringId", "role", "status", "sourcedId"],
  empty: { status: "active", role: "student" },
};

export const resultDef: ResourceDef = {
  path: "results",
  title: "Results",
  singular: "result",
  fields: [
    { key: "personId", label: "Person", kind: "ref", ref: "persons", required: true },
    { key: "courseId", label: "Course", kind: "ref", ref: "courses" },
    { key: "programId", label: "Program", kind: "ref", ref: "programs" },
    // An offering is a course given in a session: meaningless for a program.
    { key: "offeringId", label: "Offering", kind: "ref", ref: "offerings", onlyWith: "courseId" },
    { key: "grade", label: "Grade", kind: "text", placeholder: "e.g. A, VG, Pass" },
    { key: "credits", label: "Credits (ECTS)", kind: "number" },
    { key: "resultDate", label: "Result date", kind: "date" },
    statusField,
    sourcedIdField,
  ],
  columns: ["personId", "courseId", "programId", "grade", "credits", "resultDate", "status"],
  empty,
  // The server requires exactly one of courseId and programId.
  oneOf: { label: "Result for", keys: ["courseId", "programId"] },
};

// catalog lists the shared records students refer to (nobody owns them),
// in dependency order: each page only refers to records of pages before it.
export const catalog: ResourceDef[] = [
  {
    path: "organizations",
    title: "Organizations",
    singular: "organization",
    fields: [
      { key: "name", label: "Name", kind: "text", required: true },
      { key: "type", label: "Type", kind: "text", placeholder: "institution, school, department" },
      { key: "parentId", label: "Parent", kind: "ref", ref: "organizations" },
      statusField,
      sourcedIdField,
    ],
    columns: ["name", "type", "parentId", "status", "sourcedId"],
    empty,
  },
  {
    path: "academic-sessions",
    title: "Academic sessions",
    singular: "academic session",
    fields: [
      { key: "title", label: "Title", kind: "text", required: true },
      { key: "type", label: "Type", kind: "text", placeholder: "term, semester, schoolYear" },
      { key: "startDate", label: "Start date", kind: "date" },
      { key: "endDate", label: "End date", kind: "date" },
      statusField,
      sourcedIdField,
    ],
    columns: ["title", "type", "startDate", "endDate", "status", "sourcedId"],
    empty,
  },
  {
    path: "courses",
    title: "Courses",
    singular: "course",
    fields: [
      { key: "code", label: "Code", kind: "text" },
      { key: "title", label: "Title", kind: "text", required: true },
      orgField,
      statusField,
      sourcedIdField,
    ],
    columns: ["code", "title", "orgId", "status", "sourcedId"],
    empty,
  },
  {
    path: "programs",
    title: "Programs",
    singular: "program",
    fields: [
      { key: "code", label: "Code", kind: "text" },
      { key: "title", label: "Title", kind: "text", required: true },
      { key: "level", label: "Level", kind: "text", placeholder: "e.g. EQF level 6" },
      orgField,
      statusField,
      sourcedIdField,
    ],
    columns: ["code", "title", "level", "orgId", "status", "sourcedId"],
    empty,
  },
  {
    path: "offerings",
    title: "Offerings",
    singular: "offering",
    fields: [
      { key: "title", label: "Title", kind: "text" },
      { key: "courseId", label: "Course", kind: "ref", ref: "courses", required: true },
      { key: "academicSessionId", label: "Academic session", kind: "ref", ref: "academic-sessions" },
      orgField,
      statusField,
      sourcedIdField,
    ],
    columns: ["title", "courseId", "academicSessionId", "orgId", "status", "sourcedId"],
    empty,
  },
];

// refLabel is how a record of target is named in reference pickers and in
// the tables that refer to it.
export function refLabel(target: RefTarget, e: Entity): string {
  const s = (key: string) => (typeof e[key] === "string" ? (e[key] as string) : "");
  switch (target) {
    case "persons":
      return `${s("givenName")} ${s("familyName")}`.trim();
    case "organizations":
      return s("name");
    case "academic-sessions":
      return s("title");
    case "courses":
    case "programs":
      return [s("code"), s("title")].filter(Boolean).join(" ");
    case "offerings":
      return s("title") || s("sourcedId");
  }
}
