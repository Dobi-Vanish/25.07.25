# Архиватор файлов из интернета

Простая программа для добавления картинок или текстовых документов определённого формата в zip-архив для скачивания.

## Функционал
- **API Endpoints**:
  - `POST /tasks` — создание задачи.
  - `POST /tasks/{id}/files` — добавить в задачу с указанныи ID файл по ссылке. Ссылку необходимо вставлять в тело запроса.
  - `GET /tasks/{id}` — получить информацию по указанной задаче.

### Запуск локально
1. Клонируйте репозиторий:
   ```bash
   git clone https://github.com/Dobi-Vanish/25.07.25
2. Перейдите в папку с проектом и запустите при помощи команды:
   ```bash
   go run main.go
### Пример успешного запроса
 Запросы в Postman для проверки, создание задачи: 
 
 <img width="1540" height="1076" alt="изображение" src="https://github.com/user-attachments/assets/72848705-b491-4d24-8f3d-36eab5edb8de" />  
 Добавление изображений к задаче:  
 <img width="1551" height="1080" alt="изображение" src="https://github.com/user-attachments/assets/48d64a98-c014-49de-8655-4b7419dc0725" />  
 Проверка статуса задачи:  
 <img width="1538" height="1080" alt="изображение" src="https://github.com/user-attachments/assets/377668ec-a522-4178-a1e4-08412729cdd1" />  
 <img width="503" height="684" alt="изображение" src="https://github.com/user-attachments/assets/f88a8b34-4122-4d4a-8151-1e1ad1ba8d5a" />  
 
### Примечание
Для удобства для закачивания доступны файлы типа .jpg, .jpeg и .pdf. Если формать .jpg всё же недопустим - достаточно закомментировать 39 строку.  
При заполнении задачи автоматически будет создана папка "temp_archives", в которой будут храниться сгенерированные zip-файлы.  
Как я понял из задания, должно быть всего два API метода, поэтому одна из функций получилась очень большой.  
Если у Вас возникнут какие-то вопросы, замечания или что-то надо будет исправить- пожалуйста, сообщите об этом.  

