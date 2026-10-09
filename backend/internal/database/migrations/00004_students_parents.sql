-- +goose Up
-- Sprint 3: students, parents (with OTP login users), parent-student links,
-- and student route/stop assignments with history.

CREATE TABLE students (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    admission_no     citext      NOT NULL,
    name             text        NOT NULL,
    class            text        NOT NULL DEFAULT '',
    section          text        NOT NULL DEFAULT '',
    notes            text        NOT NULL DEFAULT '', -- admin-only; hidden from Transport Managers
    status           text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    -- Whether the student uses school transport at all. Assignment to a route is separate.
    transport_status text        NOT NULL DEFAULT 'uses_transport'
                     CHECK (transport_status IN ('uses_transport', 'not_using')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT students_school_admission_key UNIQUE (school_id, admission_no)
);
CREATE INDEX students_school_class_idx ON students (school_id, class, section);
CREATE TRIGGER students_updated_at BEFORE UPDATE ON students
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A parent's name and mobile live on their users row (the OTP login identity).
CREATE TABLE parents (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id        uuid        NOT NULL REFERENCES schools (id),
    user_id          uuid        NOT NULL UNIQUE REFERENCES users (id),
    alternate_mobile text        NOT NULL DEFAULT '',
    email            text        NOT NULL DEFAULT '',
    address          text        NOT NULL DEFAULT '',
    status           text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX parents_school_idx ON parents (school_id);
CREATE TRIGGER parents_updated_at BEFORE UPDATE ON parents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE parent_students (
    parent_id    uuid        NOT NULL REFERENCES parents (id) ON DELETE CASCADE,
    student_id   uuid        NOT NULL REFERENCES students (id) ON DELETE CASCADE,
    school_id    uuid        NOT NULL REFERENCES schools (id),
    relationship text        NOT NULL DEFAULT 'guardian' CHECK (relationship IN ('father', 'mother', 'guardian')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (parent_id, student_id)
);
CREATE INDEX parent_students_student_idx ON parent_students (student_id);

-- One row per assignment period. The current one has ended_at IS NULL.
-- Stops are SET NULL on delete only to keep history rows; current assignments
-- block stop deletion in the API.
CREATE TABLE student_assignments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid        NOT NULL REFERENCES schools (id),
    student_id     uuid        NOT NULL REFERENCES students (id) ON DELETE CASCADE,
    route_id       uuid        NOT NULL REFERENCES routes (id),
    pickup_stop_id uuid        REFERENCES stops (id) ON DELETE SET NULL,
    drop_stop_id   uuid        REFERENCES stops (id) ON DELETE SET NULL,
    assigned_by    uuid        REFERENCES users (id),
    assigned_at    timestamptz NOT NULL DEFAULT now(),
    ended_at       timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX student_assignments_current_key ON student_assignments (student_id) WHERE ended_at IS NULL;
CREATE INDEX student_assignments_route_idx ON student_assignments (route_id) WHERE ended_at IS NULL;
CREATE TRIGGER student_assignments_updated_at BEFORE UPDATE ON student_assignments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE student_assignments;
DROP TABLE parent_students;
DROP TABLE parents;
DROP TABLE students;
