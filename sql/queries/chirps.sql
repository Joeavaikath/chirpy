-- name: CreateChirp :one
INSERT INTO chirps (id, created_at, updated_at, body, user_id)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: DropChirps :exec
DELETE FROM chirps;

-- name: GetAllChirpsAsc :many
SELECT * FROM chirps 
ORDER BY created_at ASC;

-- name: GetAllChirpsDesc :many
SELECT * FROM chirps 
ORDER BY created_at DESC;

-- name: GetChirpById :one
SELECT * FROM chirps 
WHERE id = $1;

-- name: GetChirpsByUserIdAsc :many
SELECT * FROM chirps
WHERE user_id = $1 ORDER BY created_at ASC;

-- name: GetChirpsByUserIdDesc :many
SELECT * FROM chirps
WHERE user_id = $1 ORDER BY created_at DESC;

-- name: DeleteChirpById :exec
DELETE FROM chirps
WHERE
id = $1;