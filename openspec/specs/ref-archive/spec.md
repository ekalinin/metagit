# ref-archive Specification

## Purpose

Ни одно состояние ветки или тега, известное metagit, не теряется при force-push, удалении или перезаписи разошедшегося target'а: прежнее состояние сохраняется в архивных refs внутри metagit.

## Requirements

### Requirement: Архив перед разрушающим push
Когда metagit принимает push, который для ветки или тега не является fast-forward, удаляет ref или перемещает существующий тег, metagit MUST создать архивный ref, указывающий на прежнее значение. Имя архивного ref: `refs/metagit/archive/push/<имя ref без префикса refs/>/<время UTC в формате YYYYMMDDTHHMMSSZ>`. Если такое имя уже занято, metagit MUST добавить к нему суффикс, чтобы имя было уникальным.

#### Scenario: Force-push в ветку
- **WHEN** `refs/heads/feature` указывает на коммит C, и владелец делает force-push `feature` на коммит D, который не содержит C
- **THEN** появляется ref `refs/metagit/archive/push/heads/feature/<время>`, который указывает на C

#### Scenario: Удаление ветки
- **WHEN** владелец удаляет ветку `feature`, которая указывала на коммит C
- **THEN** появляется ref `refs/metagit/archive/push/heads/feature/<время>`, который указывает на C

#### Scenario: Перемещение тега
- **WHEN** незащищенный тег `v1.0` указывал на объект T, и владелец делает force-push `v1.0` на другой объект
- **THEN** появляется ref `refs/metagit/archive/push/tags/v1.0/<время>`, который указывает на T

#### Scenario: Fast-forward
- **WHEN** владелец делает push в ветку коммитом-потомком ее текущего значения
- **THEN** архивный ref не создается

### Requirement: Архив состояния разошедшегося target'а
Перед тем как перезаписать ref на target'е, состояние которого metagit не знает (см. `mirror-replication`), metagit MUST сохранить текущее значение этого ref на target'е в архивный ref `refs/metagit/archive/targets/<id target'а>/<имя ref без префикса refs/>/<время UTC>`. Архивный ref MUST указывать на объект из target'а, а объекты MUST храниться в репозитории metagit.

#### Scenario: Коммит, сделанный на хостинге
- **WHEN** на target'е с id 3 ветка `main` указывает на коммит E, которого нет в metagit, и metagit собирается перезаписать `main` на этом target'е
- **THEN** в metagit появляется ref `refs/metagit/archive/targets/3/heads/main/<время>`, который указывает на E, и E можно получить из metagit через git fetch

### Requirement: Сохранность архива
metagit MUST NOT удалять архивные refs автоматически. metagit MUST NOT отправлять архивные refs на target'ы. Если metagit остановился после того, как ref был изменен push'ем, но до того, как был создан архивный ref, metagit MUST создать недостающий архивный ref после перезапуска.

#### Scenario: Архив не уходит на хостинги
- **WHEN** в проекте есть архивные refs, и metagit доставляет изменения на target
- **THEN** на target'е не появляется ни одного ref из пространства `refs/metagit/`

#### Scenario: Сбой между push и архивом
- **WHEN** metagit принял force-push в ветку и был остановлен до создания архивного ref
- **THEN** после перезапуска metagit создает архивный ref на прежнее значение ветки

### Requirement: Доступ владельца к архиву
Владелец SHALL иметь возможность получить любой архивный ref через git fetch по HTTP(S) или SSH.

#### Scenario: Восстановление из архива
- **WHEN** владелец выполняет `git fetch <remote> refs/metagit/archive/push/heads/feature/<время>`
- **THEN** git получает коммит, на который указывает архивный ref, вместе с его историей
