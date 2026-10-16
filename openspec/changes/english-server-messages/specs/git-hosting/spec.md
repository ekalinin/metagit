# Spec Delta

## MODIFIED Requirements

### Requirement: Доступ к git по HTTP(S)
metagit MUST поддерживать clone, fetch и push по git smart HTTP. Каждый git-запрос по HTTP MUST быть авторизован токеном доступа владельца, переданным через HTTP Basic auth.

#### Scenario: Push с действующим токеном
- **WHEN** владелец делает `git push` по HTTP(S) с действующим токеном
- **THEN** metagit принимает изменения

#### Scenario: Запрос без токена или с неверным токеном
- **WHEN** git-клиент обращается к репозиторию без токена или с неверным либо отозванным токеном
- **THEN** metagit отвечает 401 и не отдает и не принимает данные репозитория

#### Scenario: Несуществующий проект
- **WHEN** авторизованный git-клиент обращается к проекту, которого нет
- **THEN** metagit отвечает ошибкой `repository not found`
